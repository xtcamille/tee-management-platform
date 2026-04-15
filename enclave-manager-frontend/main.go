package main

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"strings"
)

//go:embed web/*
var webFiles embed.FS

type frontendConfig struct {
	ManagerBaseURL       string `json:"managerBaseUrl"`
	DataConnectorBaseURL string `json:"dataConnectorBaseUrl"`
	ProxyBasePath        string `json:"proxyBasePath"`
	DataProxyBasePath    string `json:"dataProxyBasePath"`
	FrontendPort         string `json:"frontendPort"`
}

func main() {
	managerBaseURL := getenv("MANAGER_BASE_URL", "http://127.0.0.1:8081")
	dataConnectorBaseURL := getenv("DATA_CONNECTOR_BASE_URL", deriveDataConnectorBaseURL(managerBaseURL, "8082"))
	frontendPort := getenv("PORT", "5174")
	displayHost := getenv("DISPLAY_HOST", "127.0.0.1")

	target, err := url.Parse(managerBaseURL)
	if err != nil || target.Scheme == "" || target.Host == "" {
		log.Fatalf("invalid MANAGER_BASE_URL %q", managerBaseURL)
	}

	webRoot, err := fs.Sub(webFiles, "web")
	if err != nil {
		log.Fatalf("failed to load embedded frontend assets: %v", err)
	}

	// fileServer := http.FileServer(http.FS(webRoot))
	apiProxy := newManagerProxy(target)
	dataTarget, err := url.Parse(dataConnectorBaseURL)
	if err != nil || dataTarget.Scheme == "" || dataTarget.Host == "" {
		log.Fatalf("invalid DATA_CONNECTOR_BASE_URL %q", dataConnectorBaseURL)
	}
	dataProxy := newPathProxy("/connector", dataTarget, "data-connector")

	mux := http.NewServeMux()

	mux.Handle("/api/", apiProxy)
	mux.Handle("/connector/", dataProxy)

	mux.HandleFunc("/app-config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		writeJSON(w, http.StatusOK, frontendConfig{
			ManagerBaseURL:       managerBaseURL,
			DataConnectorBaseURL: dataConnectorBaseURL,
			ProxyBasePath:        "/api",
			DataProxyBasePath:    "/connector",
			FrontendPort:         frontendPort,
		})
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/connector/") {
			http.NotFound(w, r)
			return
		}

		cleanedPath := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/"))
		relativePath := strings.TrimPrefix(cleanedPath, "/")
		if relativePath == "" || relativePath == "." {
			relativePath = "index.html"
		}

		if _, err := fs.Stat(webRoot, relativePath); err != nil {
			if strings.Contains(path.Base(relativePath), ".") {
				http.NotFound(w, r)
				return
			}
			relativePath = "index.html"
		}

		b, err := fs.ReadFile(webRoot, relativePath)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		if strings.HasSuffix(relativePath, ".css") {
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
		} else if strings.HasSuffix(relativePath, ".js") {
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		} else {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		}

		w.Write(b)
	})

	addr := ":" + frontendPort
	log.Printf("Enclave Manager Frontend listening on http://%s%s", displayHost, addr)
	log.Printf("Proxying /api/* to %s", managerBaseURL)
	log.Printf("Proxying /connector/* to %s", dataConnectorBaseURL)

	if err := http.ListenAndServe(addr, logRequests(mux)); err != nil {
		log.Fatalf("frontend server failed to start: %v", err)
	}
}

func newManagerProxy(target *url.URL) *httputil.ReverseProxy {
	return newPathProxy("/api", target, "enclave-manager")
}

func newPathProxy(prefix string, target *url.URL, upstreamName string) *httputil.ReverseProxy {
	proxy := httputil.NewSingleHostReverseProxy(target)

	proxy.Director = func(req *http.Request) {
		trimmedPath := strings.TrimPrefix(req.URL.Path, prefix)
		if trimmedPath == "" {
			trimmedPath = "/"
		}

		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.URL.Path = joinURLPath(target.Path, trimmedPath)
		req.Host = target.Host
	}

	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("[frontend proxy] %s %s -> %s error: %v", r.Method, r.URL.Path, upstreamName, err)
		http.Error(w, "Unable to reach "+upstreamName+": "+err.Error(), http.StatusBadGateway)
	}

	return proxy
}

func joinURLPath(basePath string, relativePath string) string {
	if basePath == "" || basePath == "/" {
		if relativePath == "" {
			return "/"
		}
		return relativePath
	}
	return strings.TrimRight(basePath, "/") + "/" + strings.TrimLeft(relativePath, "/")
}

func deriveDataConnectorBaseURL(managerBaseURL string, defaultPort string) string {
	target, err := url.Parse(managerBaseURL)
	if err != nil || target.Hostname() == "" {
		return "http://127.0.0.1:" + defaultPort
	}

	scheme := target.Scheme
	if scheme == "" {
		scheme = "http"
	}

	return (&url.URL{
		Scheme: scheme,
		Host:   target.Hostname() + ":" + defaultPort,
	}).String()
}

func writeJSON(w http.ResponseWriter, statusCode int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("failed to write json response: %v", err)
	}
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("[frontend] %s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

func getenv(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
