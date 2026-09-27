package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"max-carreer-bot/internal/questionnaire"
)

const maxPreviewBody = 16 << 10

func QuestionnaireHandler(bank *questionnaire.Bank) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/questionnaire/preview", func(w http.ResponseWriter, r *http.Request) {
		requestID := newRequestID()
		w.Header().Set("X-Request-ID", requestID)
		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" || (params["charset"] != "" && !strings.EqualFold(params["charset"], "utf-8")) {
			writeError(w, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "Ожидается JSON", nil, requestID)
			return
		}
		if r.ContentLength > maxPreviewBody {
			writeError(w, http.StatusRequestEntityTooLarge, "BODY_TOO_LARGE", "Слишком большой запрос", nil, requestID)
			return
		}
		body := http.MaxBytesReader(w, r.Body, maxPreviewBody)
		defer body.Close()
		decoder := json.NewDecoder(body)
		decoder.DisallowUnknownFields()
		var selection questionnaire.Selection
		if err := decoder.Decode(&selection); err != nil {
			writeDecodeError(w, err, requestID)
			return
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			writeDecodeError(w, err, requestID)
			return
		}
		preview, err := bank.Preview(selection)
		if err != nil {
			var validation *questionnaire.ValidationError
			if errors.As(err, &validation) {
				writeError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Проверьте выбранные направления и профили", validation.Fields, requestID)
			} else {
				writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Ошибка сервера", nil, requestID)
			}
			return
		}
		writeJSON(w, http.StatusOK, preview)
	})
	return mux
}

func writeDecodeError(w http.ResponseWriter, err error, requestID string) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "BODY_TOO_LARGE", "Слишком большой запрос", nil, requestID)
		return
	}
	writeError(w, http.StatusBadRequest, "MALFORMED_JSON", "Некорректный JSON", nil, requestID)
}

func writeError(w http.ResponseWriter, status int, code, message string, fields map[string]string, requestID string) {
	type detail struct {
		Code      string            `json:"code"`
		Message   string            `json:"message"`
		Fields    map[string]string `json:"fields,omitempty"`
		RequestID string            `json:"request_id"`
	}
	writeJSON(w, status, struct {
		Error detail `json:"error"`
	}{detail{code, message, fields, requestID}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func newRequestID() string {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "unavailable"
	}
	return hex.EncodeToString(id[:])
}
