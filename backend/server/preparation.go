package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/cloudwego/hertz/pkg/app"

	"columba.local/backend/config"
	"columba.local/backend/internal/admission"
)

const preparationLeaseKey = "columbaPreparationLease"

func (s *Server) preparationLimiter() *admission.Limiter {
	s.preparationOnce.Do(func() {
		cfg := s.config
		if cfg == nil {
			cfg = &config.Config{}
		}
		limits := cfg.GetPreparationLimits()
		s.preparation = admission.New(admission.Limits{Enabled: limits.Enabled, MaxConcurrent: limits.MaxConcurrent, MaxQueued: limits.MaxQueued, MaxReservedBodyBytes: limits.MaxReservedBodyBytes, QueueTimeout: limits.QueueTimeout})
	})
	return s.preparation
}

func (s *Server) acquireGatewayPreparation(ctx context.Context, c *app.RequestContext, bytes int64) error {
	if value, exists := c.Get(preparationLeaseKey); exists && value != nil {
		return nil
	}
	lease, err := s.preparationLimiter().Acquire(ctx, bytes)
	if err == nil {
		c.Set(preparationLeaseKey, lease)
	}
	return err
}

func (s *Server) releaseGatewayPreparation(c *app.RequestContext) {
	if value, exists := c.Get(preparationLeaseKey); exists && value != nil {
		value.(*admission.Lease).Release()
		c.Set(preparationLeaseKey, nil)
	}
}

func preparationStatus(err error) int {
	if errors.Is(err, admission.ErrCapacity) {
		return http.StatusRequestEntityTooLarge
	}
	return http.StatusServiceUnavailable
}

func isPreparationFailure(err error) bool {
	return errors.Is(err, admission.ErrFull) || errors.Is(err, admission.ErrTimeout) || errors.Is(err, admission.ErrClosed) || errors.Is(err, admission.ErrCapacity)
}
