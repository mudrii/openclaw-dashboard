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

func syncRefreshCollector() {
	appserver.RefreshCollectorFunc = refreshCollectorFunc
}

func NewServer(dir, version string, cfg Config, gatewayToken string, indexHTML []byte, serverCtx context.Context) *Server {
	syncRefreshCollector()
	inner := appserver.NewServer(dir, version, cfg, gatewayToken, indexHTML, serverCtx)
	return &Server{
		inner:     inner,
		systemSvc: inner.SystemService(),
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	syncRefreshCollector()
	s.inner.ServeHTTP(w, r)
}

func (s *Server) PreWarm() {
	syncRefreshCollector()
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
