package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"drama-agent/internal/domain/fault"
)

func write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Print("response connection closed")
	}
}

func fail(w http.ResponseWriter, err error) {
	status, body := errorResponse(err)
	write(w, status, body)
}

func errorResponse(err error) (int, map[string]string) {
	status, code := 500, "PROGRAM_FAILED"
	var command *fault.Error
	switch {
	case errors.As(err, &command):
		status, code = commandStatus(command.Code), command.Code
	case errors.Is(err, fault.ErrBudget):
		status, code = 409, "BUDGET_EXHAUSTED"
	case errors.Is(err, fault.ErrModelUnavailable):
		status, code = 503, "MODEL_UNAVAILABLE"
	case errors.Is(err, fault.ErrNotFound):
		status, code = 404, "NOT_FOUND"
	case errors.Is(err, fault.ErrConflict):
		status, code = 409, "CONFLICT"
	case errors.Is(err, fault.ErrInvalid), errors.Is(err, fault.ErrInvalidDocument), errors.Is(err, fault.ErrUnsupportedFormat):
		status, code = 400, "INVALID_INPUT"
	}
	message := err.Error()
	if status == 500 {
		message = "服务发生错误；未报告成功，详见本地运行日志"
		log.Printf("catalog API error: %v", err)
	}
	return status, map[string]string{"code": code, "message": message}
}
