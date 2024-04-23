package server

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/ollama/ollama/api"
	"github.com/ollama/ollama/convert"
	"github.com/ollama/ollama/llm"
	"github.com/ollama/ollama/types/model"
	"github.com/ollama/ollama/types/ordered"
)

func parseFromModel(ctx context.Context, name model.Name, fn func(api.ProgressResponse)) (*ordered.Map[*Layer, *llm.GGML], error) {
	modelpath := ParseModelPath(name.DisplayLongest())
	manifest, _, err := GetManifest(modelpath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := PullModel(ctx, name.DisplayLongest(), &registryOptions{}, fn); err != nil {
			return nil, err
		}

		modelpath = ParseModelPath(name.DisplayLongest())
		manifest, _, err = GetManifest(modelpath)
		if err != nil {
			return nil, err
		}
	case err != nil:
		return nil, err
	}

	layers := ordered.NewMap[*Layer, *llm.GGML]()
	for _, layer := range manifest.Layers {
		layer, err := NewLayerFromLayer(layer.Digest, layer.MediaType, modelpath.GetShortTagname())
		if err != nil {
			return nil, err
		}

		switch layer.MediaType {
		case "application/vnd.ollama.image.model",
			"application/vnd.ollama.image.projector",
			"application/vnd.ollama.image.adapter":
			blobpath, err := GetBlobsPath(layer.Digest)
			if err != nil {
				return nil, err
			}

			blob, err := os.Open(blobpath)
			if err != nil {
				return nil, err
			}
			defer blob.Close()

			ggml, _, err := llm.DecodeGGML(blob)
			if err != nil {
				return nil, err
			}
			layers.Add(layer, ggml)
		default:
			layers.Add(layer, nil)
		}

	}

	return layers, nil
}

func parseFromZipFile(_ context.Context, file *os.File, fn func(api.ProgressResponse)) (*ordered.Map[*Layer, *llm.GGML], error) {
	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}

	r, err := zip.NewReader(file, stat.Size())
	if err != nil {
		return nil, err
	}

	tempdir, err := os.MkdirTemp(filepath.Dir(file.Name()), "")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempdir)

	fn(api.ProgressResponse{Status: "unpacking model metadata"})
	for _, f := range r.File {
		// TODO(mxyng): this should not write out all files to disk
		outfile, err := os.Create(filepath.Join(tempdir, f.Name))
		if err != nil {
			return nil, err
		}

		infile, err := f.Open()
		if err != nil {
			return nil, err
		}

		if _, err = io.Copy(outfile, infile); err != nil {
			return nil, err
		}

		if err := outfile.Close(); err != nil {
			return nil, err
		}

		if err := infile.Close(); err != nil {
			return nil, err
		}
	}

	mf, err := convert.GetModelFormat(tempdir)
	if err != nil {
		return nil, err
	}

	params, err := mf.GetParams(tempdir)
	if err != nil {
		return nil, err
	}

	mArch, err := mf.GetModelArch("", tempdir, params)
	if err != nil {
		return nil, err
	}

	fn(api.ProgressResponse{Status: "processing tensors"})
	if err := mArch.GetTensors(); err != nil {
		return nil, err
	}

	if err := mArch.LoadVocab(); err != nil {
		return nil, err
	}

	fn(api.ProgressResponse{Status: "converting model"})

	// TODO(mxyng): this should write directly into a layer
	// e.g. NewLayer(arch.Reader(), "application/vnd.ollama.image.model")
	temp, err := os.CreateTemp(tempdir, "fp16")
	if err != nil {
		return nil, err
	}
	defer temp.Close()
	defer os.Remove(temp.Name())

	if err = mArch.WriteGGUF(temp); err != nil {
		return nil, err
	}

	if _, err := temp.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	layer, err := NewLayer(temp, "application/vnd.ollama.image.model")
	if err != nil {
		return nil, fmt.Errorf("aaa: %w", err)
	}

	blobpath, err := GetBlobsPath(layer.Digest)
	if err != nil {
		return nil, err
	}

	bin, err := os.Open(blobpath)
	if err != nil {
		return nil, err
	}
	defer bin.Close()

	ggml, _, err := llm.DecodeGGML(bin)
	if err != nil {
		return nil, err
	}

	layer, err = NewLayerFromLayer(layer.Digest, layer.MediaType, "")
	if err != nil {
		return nil, err
	}

	layers := ordered.NewMap[*Layer, *llm.GGML]()
	layers.Add(layer, ggml)
	return layers, nil
}

func parseFromFile(ctx context.Context, file *os.File, fn func(api.ProgressResponse)) (*ordered.Map[*Layer, *llm.GGML], error) {
	sr := io.NewSectionReader(file, 0, 512)
	contentType, err := detectContentType(sr)
	if err != nil {
		return nil, err
	}

	switch contentType {
	case "gguf", "ggla":
		// noop
	case "application/zip":
		return parseFromZipFile(ctx, file, fn)
	default:
		return nil, fmt.Errorf("unsupported content type: %s", contentType)
	}

	layers := ordered.NewMap[*Layer, *llm.GGML]()

	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}

	var offset int64
	for offset < stat.Size() {
		ggml, n, err := llm.DecodeGGML(file)
		if errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, err
		}

		mediatype := "application/vnd.ollama.image.model"
		if ggml.Name() == "ggla" {
			mediatype = "application/vnd.ollama.image.adapter"
		} else if ggml.KV().Architecture() == "clip" {
			mediatype = "application/vnd.ollama.image.projector"
		}

		layer, err := NewLayer(io.NewSectionReader(file, offset, n), mediatype)
		if err != nil {
			return nil, err
		}

		layers.Add(layer, ggml)
		offset = n
	}

	return layers, nil
}

func detectContentType(r io.Reader) (string, error) {
	var b bytes.Buffer
	if _, err := io.Copy(&b, r); err != nil {
		return "", err
	}

	if contentType := llm.DetectGGMLType(b.Bytes()); contentType != "" {
		return contentType, nil
	}

	if contentType := http.DetectContentType(b.Bytes()); contentType != "application/octet-stream" {
		return contentType, nil
	}

	return "unknown", nil
}
