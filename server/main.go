package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type APIError struct {
	err    error
	status int
}

func (e *APIError) Error() string {
	var msg string

	if err := e.Unwrap(); err != nil {
		msg = err.Error()
	} else {
		msg = http.StatusText(e.StatusCode())
	}

	if msg == "" {
		return "undefined"
	}
	return msg
}

func (e *APIError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func (e *APIError) StatusCode() int {
	if e == nil || e.status < 100 || e.status > 599 {
		return http.StatusInternalServerError
	}
	return e.status
}

func newAPIError(err error, status int) *APIError {
	return &APIError{err, status}
}

func newAPIErrorFrom(err error) *APIError {
	if err == nil {
		return newAPIError(nil, http.StatusInternalServerError)
	}

	if impl, ok := err.(*APIError); ok || errors.As(err, &impl) {
		return newAPIError(impl.Unwrap(), impl.StatusCode())
	}

	if errors.Is(err, context.Canceled) {
		return newAPIError(err, http.StatusInternalServerError)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return newAPIError(err, http.StatusGatewayTimeout)
	}

	if impl, ok := err.(*json.SyntaxError); ok || errors.As(err, &impl) {
		return newAPIError(err, http.StatusBadRequest)
	}

	msg := err.Error()

	if strings.Contains(msg, "request body too large") {
		return newAPIError(err, http.StatusRequestEntityTooLarge)
	}

	return newAPIError(err, http.StatusInternalServerError)
}

type CompositeOptions struct {
	Width       int64 `json:"width"`
	Height      int64 `json:"height"`
	FrameCount  int64 `json:"frameCount"`
	FrameDelay  int64 `json:"frameDelay"`
	FlagCrop    bool  `json:"flagCrop"`
	FlagReverse bool  `json:"flagReverse"`
	FlagFlatten bool  `json:"flagFlatten"`
	Background  int64 `json:"background"`
}

type CompositeLimits struct {
	SizeMax       int64 `json:"sizeMax"`
	WidthMax      int64 `json:"widthMax"`
	HeightMax     int64 `json:"heightMax"`
	FrameCountMin int64 `json:"frameCountMin"`
	FrameCountMax int64 `json:"frameCountMax"`
	FrameDelayMin int64 `json:"frameDelayMin"`
	FrameDelayMax int64 `json:"frameDelayMax"`
}

func (opts *CompositeOptions) Validate(lims *CompositeLimits) error {
	if opts.Width > lims.WidthMax {
		return fmt.Errorf("width %d is larger than maximum %d", opts.Width, lims.WidthMax)
	}
	if opts.Height > lims.HeightMax {
		return fmt.Errorf("height %d is larger than maximum %d", opts.Height, lims.HeightMax)
	}
	if opts.FrameCount < lims.FrameCountMin {
		return fmt.Errorf("frameCount %d is smaller than minimum %d", opts.FrameCount, lims.FrameCountMin)
	}
	if opts.FrameCount > lims.FrameCountMax {
		return fmt.Errorf("frameCount %d is larger than maximum %d", opts.FrameCount, lims.FrameCountMax)
	}
	if opts.FrameDelay < lims.FrameDelayMin {
		return fmt.Errorf("frameDelay %d is smaller than minimum %d", opts.FrameDelay, lims.FrameDelayMin)
	}
	if opts.FrameDelay > lims.FrameDelayMax {
		return fmt.Errorf("frameDelay %d is larger than maximum %d", opts.FrameDelay, lims.FrameDelayMax)
	}
	return nil
}

type CompositeTask struct {
	Options    *CompositeOptions
	PathInput  string
	PathOutput string
	PathBinary string
}

func (task *CompositeTask) Execute(ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			if rerr, ok := r.(error); ok && rerr != nil {
				err = fmt.Errorf("task panic: %w", rerr)
			} else {
				err = fmt.Errorf("task panic: %v", r)
			}
		}
	}()

	ctx_, cancel := context.WithCancel(ctx)
	defer cancel()

	argInt := func(val int64) string { return strconv.FormatInt(val, 10) }
	argFlag := func(val bool) string { if val { return "1" }; return "0" }

	opts := task.Options
	args := []string{
		argInt(opts.Width),
		argInt(opts.Height),
		argInt(opts.FrameCount),
		argInt(opts.FrameDelay),
		argFlag(opts.FlagCrop),
		argFlag(opts.FlagReverse),
		argFlag(opts.FlagFlatten),
		argInt(opts.Background),
		task.PathInput,
		task.PathOutput,
	}

	command := exec.CommandContext(ctx_, task.PathBinary, args...)

	output := new(bytes.Buffer)
	command.Stdout = output
	command.Stderr = output

	if err := command.Start(); err != nil {
		select {
		case <- ctx.Done():
			return ctx.Err()
		default:
			return fmt.Errorf("task failed to start: %w", err)
		}
	}

	if err := command.Wait(); err != nil {
		select {
		case <- ctx.Done():
			return ctx.Err()
		default:
			return fmt.Errorf("task failed: %q, %w", strings.TrimSpace(output.String()), err)
		}
	}

	return ctx.Err()
}

type CompositeDispatch struct {
	Semaphore chan struct{}
}

func newCompositeDispatch(size int) *CompositeDispatch {
	return &CompositeDispatch{make(chan struct{}, size)}
}

func (dispatch *CompositeDispatch) Release() { <- dispatch.Semaphore }

func (dispatch *CompositeDispatch) Execute(ctx context.Context, task *CompositeTask) error {
	select {
	case <- ctx.Done():
		return ctx.Err()
	case dispatch.Semaphore <- struct{}{}:
		defer dispatch.Release()
		return task.Execute(ctx)
	}
}

type ContextReader struct {
	r   io.Reader
	ctx context.Context
}

func newContextReader(r io.Reader, ctx context.Context) io.Reader {
	return &ContextReader{r, ctx}
}

func (r *ContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

var nanoidSource = bufio.NewReader(rand.Reader)
var nanoidEncode = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567")

func nanoid() string {
	b := make([]byte, 10)
	_, err := io.ReadFull(nanoidSource, b)
	if err != nil {
		panic(fmt.Errorf("nanoid io.ReadFull: %w", err))
	}
	return nanoidEncode.EncodeToString(b)
}

var replaceUnsafePattern = regexp.MustCompile("[^a-zA-Z0-9.~_-]")

func replaceUnsafe(s string) string {
	return replaceUnsafePattern.ReplaceAllString(s, "_")
}

func generateTokenInput(extension string) string {
	return "/temp/" + nanoid() + "." + extension
}

func generateTokenOutput(token string) string {
	tokenBase := path.Base(token)
	tokenExt := path.Ext(tokenBase)
	return "/temp/" + tokenBase[:len(tokenBase) - len(tokenExt)] + "-" + nanoid() + ".gif"
}

func getPathFromToken(pathTemp string, token string) string {
	return filepath.Join(pathTemp, path.Base(token))
}

func getExtensionForMIME(contentType string) string {
	if contentType == "" {
		return ""
	}
	if strings.Contains(contentType, "image/png") {
		return "png"
	}
	if strings.Contains(contentType, "image/jpeg") {
		return "jpg"
	}
	if strings.Contains(contentType, "image/gif") {
		return "gif"
	}
	if strings.Contains(contentType, "image/webp") {
		return "webp"
	}
	return ""
}

func sendJSON(res http.ResponseWriter, status int, value interface{}) {
	b, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Errorf("sendJSON json.Marshal: %w", err))
	}
	res.Header().Set("Content-Length", strconv.Itoa(len(b)))
	res.Header().Set("Content-Type", "application/json")
	res.WriteHeader(status)
	res.Write(b)
}

func sendResult(res http.ResponseWriter, value interface{}) {
	sendJSON(res, http.StatusOK, value)
}

func sendError(res http.ResponseWriter, err error) {
	wrapped := newAPIErrorFrom(err)
	sendErrorMessage(res, wrapped.StatusCode(), wrapped.Error())
}

func sendErrorMessage(res http.ResponseWriter, status int, message string) {
	res.Header().Set("X-Spin-Error", message)
	sendJSON(res, status, map[string]string{"err": message})
}

func handleUpload(res http.ResponseWriter, req *http.Request) {
	contentLength := req.ContentLength
	contentLengthMax := settings.Limits.SizeMax

	if contentLength < 1 {
		sendErrorMessage(res, http.StatusLengthRequired, "content-length required")
		return
	}
	if contentLength > contentLengthMax {
		sendErrorMessage(res, http.StatusRequestEntityTooLarge, fmt.Sprintf("content-length %d is larger than maximum %d", contentLength, contentLengthMax))
		return
	}

	bodyBuffered := bufio.NewReaderSize(http.MaxBytesReader(res, req.Body, contentLength), 65536)
	body := newContextReader(bodyBuffered, req.Context())

	extension := getExtensionForMIME(req.Header.Get("Content-Type"))
	if extension == "" {
		b, err := bodyBuffered.Peek(512)
		if err == bufio.ErrBufferFull {
			sendErrorMessage(res, http.StatusBadRequest, "image data too small")
			return
		}
		if err != nil {
			sendError(res, fmt.Errorf("handleUpload body.Peek: %w", err))
			return
		}

		extension = getExtensionForMIME(http.DetectContentType(b))
		if extension == "" {
			sendErrorMessage(res, http.StatusBadRequest, "image format unrecognized")
			return
		}
	}

	token := generateTokenInput(extension)

	res.Header().Set("X-Spin-Token", token)

	var err error

	tempPath := getPathFromToken(settings.PathTemp, token)
	temp, err := os.OpenFile(tempPath, os.O_CREATE | os.O_WRONLY, 0644)
	if err != nil {
		sendError(res, fmt.Errorf("handleUpload os.OpenFile: %w", err))
		return
	}

	defer temp.Close()

	_, err = io.Copy(temp, body)
	if err != nil {
		sendError(res, fmt.Errorf("handeUpload io.Copy: %w", err))
		return
	}

	sendResult(res, map[string]string{"file": token})
}

func handleSpin(res http.ResponseWriter, req *http.Request) {
	var tokenInput string
	var tokenOutput string

	if tokenInput = req.URL.Query().Get("file"); tokenInput == "" {
		sendErrorMessage(res, http.StatusBadRequest, "query parameter \"file\" required")
		return
	}
	tokenOutput = generateTokenOutput(tokenInput)

	pathInput := getPathFromToken(settings.PathTemp, tokenInput)
	pathOutput := getPathFromToken(settings.PathTemp, tokenOutput)

	res.Header().Set("X-Spin-Token", tokenInput)
	res.Header().Set("X-Spin-TokenOutput", tokenOutput)

	var err error

	_, err = os.Stat(pathInput)
	if errors.Is(err, os.ErrNotExist) {
		sendErrorMessage(res, http.StatusNotFound, "query parameter \"file\" not found")
		return
	}

	ctx := req.Context()

	body := newContextReader(http.MaxBytesReader(res, req.Body, 65536), ctx)

	var opts *CompositeOptions
	var task *CompositeTask

	err = json.NewDecoder(body).Decode(&opts)
	if err != nil {
		sendError(res, fmt.Errorf("invalid json: %w", err))
		return
	}

	err = opts.Validate(settings.Limits)
	if err != nil {
		sendError(res, newAPIError(fmt.Errorf("invalid settings: %w", err), http.StatusBadRequest))
		return
	}

	task = new(CompositeTask)
	task.Options = opts
	task.PathInput = pathInput
	task.PathOutput = pathOutput
	task.PathBinary = settings.PathBinary

	err = dispatch.Execute(ctx, task)
	if err != nil {
		sendError(res, err)
		return
	}

	sendResult(res, map[string]string{"file": tokenOutput})
}

type Settings struct {
	Limits     *CompositeLimits

	PathTemp   string
	PathPublic string
	PathBinary string

	DispatchSize int64
	ShutdownMS   int64
	TimeoutMS    int64

	ListenAddr   string
}

var logger *log.Logger
var settings *Settings
var dispatch *CompositeDispatch

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int64) int64 {
	s := getEnv(key, "")
	i, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fallback
	}
	return i
}

func main() {
	logger = log.New(os.Stdout, "", log.LstdFlags)
	logger.Println("Server starting")

	settings = new(Settings)

	settings.Limits = new(CompositeLimits)
	settings.Limits.SizeMax = getEnvInt("LIMIT_MAX_SIZE", 5 * 1024 * 1024)
	settings.Limits.WidthMax = getEnvInt("LIMIT_MAX_WIDTH", 1024)
	settings.Limits.HeightMax = getEnvInt("LIMIT_MAX_HEIGHT", 1024)
	settings.Limits.FrameCountMin = getEnvInt("LIMIT_MIN_FRAME_COUNT", 2)
	settings.Limits.FrameCountMax = getEnvInt("LIMIT_MAX_FRAME_COUNT", 120)
	settings.Limits.FrameDelayMin = getEnvInt("LIMIT_MIN_FRAME_DELAY", 5)
	settings.Limits.FrameDelayMax = getEnvInt("LIMIT_MAX_FRAME_DELAY", 1000)

	settings.PathTemp = getEnv("PATH_TEMP", "/data")
	settings.PathPublic = getEnv("PATH_PUBLIC", "/app/public")
	settings.PathBinary = getEnv("PATH_BINARY", "/app/bin/gifspin-core")

	settings.DispatchSize = getEnvInt("OPT_DISPATCH_SIZE", 4)
	settings.ShutdownMS = getEnvInt("OPT_SHUTDOWN_MS", 20000)
	settings.TimeoutMS = getEnvInt("OPT_TIMEOUT_MS", 15000)

	settings.ListenAddr = getEnv("LISTEN", ":3000")

	dispatch = newCompositeDispatch(int(settings.DispatchSize))

	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.CleanPath)
	r.Use(middleware.StripSlashes)
	r.Use(middleware.GetHead)

	r.Use(middleware.SetHeader("X-Powered-By", "a cute foxgirl :3 https://foxgirl.dev/"))

	r.Route("/api", func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
				ctx, cancel := context.WithTimeout(req.Context(), time.Duration(settings.TimeoutMS) * time.Millisecond)
				defer cancel()
				next.ServeHTTP(res, req.WithContext(ctx))
			})
		})

		r.Post("/upload", handleUpload)
		r.Post("/spin", handleSpin)

		r.Get("/limits", func(res http.ResponseWriter, req *http.Request) {
			sendResult(res, settings.Limits)
		})

		r.NotFound(func(res http.ResponseWriter, req *http.Request) {
			sendError(res, newAPIError(nil, http.StatusNotFound))
		})
		r.MethodNotAllowed(func(res http.ResponseWriter, req *http.Request) {
			sendError(res, newAPIError(nil, http.StatusMethodNotAllowed))
		})
	})

	r.Get("/temp/{filename}", func(res http.ResponseWriter, req *http.Request) {
		filename := chi.URLParam(req, "filename")
		attachment := req.URL.Query().Get("attachment")

		if attachment != "" {
			headerName := "Content-Disposition"
			headerValue := "attachment; filename=\"" + replaceUnsafe(attachment) + "\""
			res.Header().Set(headerName, headerValue)
		}

		http.ServeFile(res, req, filepath.Join(settings.PathTemp, filename))
	})

	r.NotFound(http.FileServer(http.Dir(settings.PathPublic)).ServeHTTP)

	ctx, cancel := context.WithCancel(context.Background())

	server := &http.Server{
		Addr: settings.ListenAddr,

		Handler: r,
		ErrorLog: logger,

		BaseContext: func(l net.Listener) context.Context { return ctx },

		IdleTimeout: 30 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,

		MaxHeaderBytes: 65536,
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	go func() {
		<- sig

		ctxTimeout, cancelTimeout := context.WithTimeout(ctx, time.Duration(settings.ShutdownMS) * time.Millisecond)

		defer cancel()
		defer cancelTimeout()

		go func() {
			<- ctxTimeout.Done()
			if ctxTimeout.Err() == context.DeadlineExceeded {
				logger.Println("Server shutdown took too long, exiting")
			}
		}()

		if err := server.Shutdown(ctxTimeout); err != nil {
			logger.Println(fmt.Sprintf("Server shutdown failed, exiting: %s", err))
		}

		logger.Println("Server shutdown complete")
	}()

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Println(fmt.Printf("Server encountered unexpected error: %v", err))
	} else {
		logger.Println("Server closed")
	}

	<- ctx.Done()
}
