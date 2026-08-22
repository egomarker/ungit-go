//go:build !linux

package server

import "context"

func (s *Server) watchRepository(ctx context.Context, c *realtimeClient, repoPath string, ready chan<- struct{}) {
	s.watchRepositoryPolling(ctx, c, repoPath, ready)
}
