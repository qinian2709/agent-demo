package main

import (
	"agent-demo/eval"
	"encoding/json"
	"fmt"
	"net/http"
)

func registerEvalRoutes(mux *http.ServeMux, svc *eval.Service) {
	mux.HandleFunc("/api/tools", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"tools": svc.Agent.Tools().Specs(),
		})
	})

	mux.HandleFunc("/api/eval/cases", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]interface{}{"cases": svc.GetCases()})
	})

	mux.HandleFunc("/api/eval/report", func(w http.ResponseWriter, r *http.Request) {
		report := svc.GetReport()
		if report == nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{"report": nil})
			return
		}
		writeJSON(w, http.StatusOK, report)
	})

	mux.HandleFunc("/api/eval/generate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req eval.GenerateRequest
		req.UseLLM = true
		_ = json.NewDecoder(r.Body).Decode(&req)
		resp, err := eval.Generate(r.Context(), svc.Agent.Tools().Specs(), svc.Agent.LLM(), req.UseLLM, req.Focus)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		svc.SetCases(resp.Cases)
		writeJSON(w, http.StatusOK, resp)
	})

	mux.HandleFunc("/api/eval/run", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req eval.RunRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		stream := r.Header.Get("Accept") == "text/event-stream" || r.URL.Query().Get("stream") == "1"
		if !stream {
			report := svc.Run(r.Context(), req.Cases, nil)
			writeJSON(w, http.StatusOK, report)
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			report := svc.Run(r.Context(), req.Cases, nil)
			writeJSON(w, http.StatusOK, report)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		writeSSE(w, flusher, "start", map[string]int{"total": len(req.Cases)})
		report := svc.Run(r.Context(), req.Cases, func(cr eval.CaseResult) {
			writeSSE(w, flusher, "case", cr)
		})
		writeSSE(w, flusher, "done", report)
	})
}

func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, event string, body interface{}) {
	payload, err := json.Marshal(body)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, payload)
	flusher.Flush()
}
