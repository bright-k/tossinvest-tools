package runtime

import (
	"context"
	"github.com/bright-k/tossinvest-tools/internal/broker/toss"
	"github.com/bright-k/tossinvest-tools/internal/core"
)

func attachStreams(ctx context.Context, s *Server) error {
	url := toss.WSURL
	r := s.Service.Broker.(*toss.REST)
	if s.mock != nil {
		url = s.mock.WSURL()
	}
	streams := core.NewStreams(s.Service, core.StreamOptions{URL: url, Token: r.Token})
	s.Service.Streams = streams
	streams.Start(ctx)
	return nil
}
func closeStreams(s *Server) {
	if x, ok := s.Service.Streams.(*core.Streams); ok {
		x.Close()
	}
}
