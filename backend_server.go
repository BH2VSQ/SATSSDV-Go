package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
)

type backendHandshake struct {
	Address string `json:"address"`
	Token   string `json:"token"`
	PID     int    `json:"pid"`
}

type rpcRequest struct {
	Method string            `json:"method"`
	Args   []json.RawMessage `json:"args"`
}

type rpcResponse struct {
	OK    bool        `json:"ok"`
	Data  interface{} `json:"data,omitempty"`
	Error string      `json:"error,omitempty"`
}

type eventsResponse struct {
	Events []AppEvent `json:"events"`
	Next   uint64     `json:"next"`
	Reset  bool       `json:"reset"`
}

func runBackend() {
	log.SetOutput(os.Stderr)
	ctx, cancel := signalContext()
	defer cancel()
	app := NewApp()
	app.startup(ctx)
	defer app.shutdown(context.Background())

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Printf("listen for local UI: %v", err)
		return
	}
	var tokenBytes [32]byte
	if _, err := rand.Read(tokenBytes[:]); err != nil {
		_ = listener.Close()
		log.Printf("generate local UI token: %v", err)
		return
	}
	token := hex.EncodeToString(tokenBytes[:])
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "service": "satssdv-go-backend"})
	})
	mux.HandleFunc("POST /rpc", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r, token) {
			writeJSON(w, http.StatusUnauthorized, rpcResponse{Error: "unauthorized"})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 24<<20)
		defer r.Body.Close()
		var request rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeJSON(w, http.StatusBadRequest, rpcResponse{Error: "invalid RPC request: " + err.Error()})
			return
		}
		data, callErr := invokeApp(app, request)
		if callErr != nil {
			writeJSON(w, http.StatusOK, rpcResponse{OK: false, Error: callErr.Error()})
			return
		}
		writeJSON(w, http.StatusOK, rpcResponse{OK: true, Data: data})
	})
	mux.HandleFunc("POST /shutdown", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r, token) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		go cancel()
	})
	mux.HandleFunc("GET /events", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r, token) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		after, _ := strconv.ParseUint(r.URL.Query().Get("after"), 10, 64)
		events, next, reset := app.events.Since(after)
		writeJSON(w, http.StatusOK, eventsResponse{Events: events, Next: next, Reset: reset})
	})
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		_ = server.Shutdown(shutdownCtx)
	}()
	info := backendHandshake{Address: "http://" + listener.Addr().String(), Token: token, PID: os.Getpid()}
	if err := json.NewEncoder(os.Stdout).Encode(info); err != nil {
		_ = listener.Close()
		log.Printf("write Qt backend handshake: %v", err)
		return
	}
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("local UI server stopped: %v", err)
	}
}

func signalContext() (context.Context, context.CancelFunc) {
	return newSignalContext()
}

func authorized(r *http.Request, token string) bool {
	provided := r.Header.Get("X-SATSSDV-Token")
	return len(provided) == len(token) && subtle.ConstantTimeCompare([]byte(provided), []byte(token)) == 1
}

var rpcMethodAllowlist = map[string]bool{
	"GetProfiles": true, "GetSettings": true, "SaveSettings": true,
	"GetAudioState": true, "GetAudioDevices": true, "StartRX": true, "StopRX": true,
	"StopTX": true, "Start1500HzTestTone": true, "StartTXRendered": true,
	"GetSerialPorts": true, "RefreshSerialPorts": true, "StartPTTTest": true, "StopPTTTest": true,
	"GetPTTTestState": true, "GetPTTError": true, "GetLocales": true, "GetLocaleDictionary": true,
	"GetTemplates": true, "SaveTemplates": true, "GetAlbum": true, "GetGallery": true,
	"ImportGalleryImage": true, "GetGalleryImageData": true, "GetLatestRXImageData": true, "GetLatestRXGIFData": true,
	"DeleteGalleryImage": true, "GetAlbumImageData": true, "DeleteAlbumRecord": true,
	"ExportAlbumRecordToDirectory": true, "ExportAlbumToDirectoryPath": true,
	"ExportTemplatePackTo": true, "ImportTemplatePackFrom": true,
}

func invokeApp(app *App, request rpcRequest) (result interface{}, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = nil
			err = fmt.Errorf("RPC %s panicked: %v", request.Method, recovered)
		}
	}()
	name := strings.TrimSpace(request.Method)
	if name == "" || strings.HasPrefix(name, "_") {
		return nil, errors.New("RPC method is empty or invalid")
	}
	if !rpcMethodAllowlist[name] {
		return nil, fmt.Errorf("RPC method %q is not exposed to the native UI", name)
	}
	method := reflect.ValueOf(app).MethodByName(name)
	if !method.IsValid() {
		return nil, fmt.Errorf("unknown RPC method %q", name)
	}
	methodType := method.Type()
	if methodType.NumIn() != len(request.Args) {
		return nil, fmt.Errorf("RPC method %s expects %d arguments, got %d", name, methodType.NumIn(), len(request.Args))
	}
	args := make([]reflect.Value, len(request.Args))
	for i, raw := range request.Args {
		argType := methodType.In(i)
		arg := reflect.New(argType)
		if len(raw) == 0 {
			return nil, fmt.Errorf("RPC method %s argument %d is missing", name, i)
		}
		if err := json.Unmarshal(raw, arg.Interface()); err != nil {
			return nil, fmt.Errorf("RPC method %s argument %d: %w", name, i, err)
		}
		args[i] = arg.Elem()
	}
	outputs := method.Call(args)
	if len(outputs) == 0 {
		return nil, nil
	}
	if last := outputs[len(outputs)-1]; last.Type().Implements(reflect.TypeOf((*error)(nil)).Elem()) {
		if !last.IsNil() {
			return nil, last.Interface().(error)
		}
		outputs = outputs[:len(outputs)-1]
	}
	if len(outputs) == 0 {
		return nil, nil
	}
	if len(outputs) == 1 {
		return outputs[0].Interface(), nil
	}
	values := make([]interface{}, len(outputs))
	for i := range outputs {
		values[i] = outputs[i].Interface()
	}
	return values, nil
}

func writeJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
