package dashboard

import (
	"context"
	"net/http"

	appserver "github.com/mudrii/openclaw-dashboard/internal/appserver"
)

const (
	maxBodyBytes   = 64 * 1024
	maxQuestionLen = 2000
	chatRateLimit  = 10
)

type Server struct {
	inner     *appserver.Server
	systemSvc *SystemService
}

func NewServer(dir, version string, cfg Config, gatewayToken string, indexHTML []byte, serverCtx context.Context) *Server {
	inner := appserver.NewServer(dir, version, cfg, gatewayToken, indexHTML, serverCtx, refreshCollectorFunc)
	return &Server{
		inner:     inner,
		systemSvc: inner.SystemService(),
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.inner.ServeHTTP(w, r)
}

func (s *Server) PreWarm() {
	s.inner.PreWarm()
}

func (s *Server) getDataCached() (map[string]any, error) {
	return s.inner.GetDataCached()
}

func (s *Server) getDataRawCached() ([]byte, error) {
	return s.inner.GetDataRawCached()
}

func (s *Server) handleStaticFile(w http.ResponseWriter, r *http.Request, path, contentType string) {
	s.inner.HandleStaticFile(w, r, path, contentType)
}
