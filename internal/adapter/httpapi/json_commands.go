package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"drama-agent/internal/application/validation"
	"drama-agent/internal/domain/fault"
)

func readJSONBody(w http.ResponseWriter, r *http.Request, v any) ([]byte, error) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		var size *http.MaxBytesError
		if errors.As(err, &size) {
			return nil, fault.New("REQUEST_TOO_LARGE", "JSON 命令上限 1 MiB")
		}
		return nil, fault.ErrInvalid
	}
	if err = validation.DecodeStrict(raw, v); err != nil {
		return nil, err
	}
	return raw, nil
}

func readCommand(w http.ResponseWriter, r *http.Request, v any) error {
	raw, err := readJSONBody(w, r, v)
	if err != nil {
		return err
	}
	var revision struct {
		Revision uint64 `json:"revision"`
	}
	if json.Unmarshal(raw, &revision) != nil || revision.Revision == 0 {
		return fault.ErrInvalid
	}
	return nil
}

func revision(w http.ResponseWriter, r *http.Request) (uint64, error) {
	var body struct {
		Revision uint64 `json:"revision"`
	}
	if err := readCommand(w, r, &body); err != nil {
		return 0, err
	}
	if body.Revision == 0 {
		return 0, fault.ErrInvalid
	}
	return body.Revision, nil
}

func readPayload(w http.ResponseWriter, r *http.Request, v any) error {
	_, err := readJSONBody(w, r, v)
	return err
}

func generationRevision(w http.ResponseWriter, r *http.Request) (uint64, string, error) {
	var in struct {
		Revision     uint64 `json:"revision"`
		BillingBasis string `json:"billingBasis,omitempty"`
	}
	if e := readCommand(w, r, &in); e != nil {
		return 0, "", e
	}
	return in.Revision, in.BillingBasis, nil
}
