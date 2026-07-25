package proxy

import (
	"context"
	"log/slog"
	"time"

	"github.com/chris/llm-router/internal/repository"
)

type ReenableExpirer struct {
	modelRepo    repository.ModelRepository
	providerRepo repository.ProviderRepository
	tickInterval time.Duration
	logger       *slog.Logger
	stopCh       chan struct{}
}

func NewReenableExpirer(modelRepo repository.ModelRepository, providerRepo repository.ProviderRepository, logger *slog.Logger) *ReenableExpirer {
	return &ReenableExpirer{
		modelRepo:    modelRepo,
		providerRepo: providerRepo,
		tickInterval: 30 * time.Second,
		logger:       logger,
		stopCh:       make(chan struct{}),
	}
}

func (e *ReenableExpirer) Start(ctx context.Context) {
	ticker := time.NewTicker(e.tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-e.stopCh:
			return
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			e.reenableExpired(ctx, now)
		}
	}
}

func (e *ReenableExpirer) Stop() {
	close(e.stopCh)
}

func (e *ReenableExpirer) reenableExpired(ctx context.Context, now time.Time) {
	modelIDs, err := e.modelRepo.ListExpiredDisabled(ctx, now)
	if err != nil {
		e.logger.Error("expirer: failed to list expired models", "error", err)
	} else {
		for _, id := range modelIDs {
			if err := e.modelRepo.ToggleDisabled(ctx, id, false, nil); err != nil {
				e.logger.Error("expirer: failed to re-enable model", "id", id, "error", err)
			} else {
				e.logger.Info("expirer: auto re-enabled model", "id", id)
			}
		}
	}

	providerIDs, err := e.providerRepo.ListExpiredDisabled(ctx, now)
	if err != nil {
		e.logger.Error("expirer: failed to list expired providers", "error", err)
	} else {
		for _, id := range providerIDs {
			p, err := e.providerRepo.GetByID(ctx, id)
			if err != nil || p == nil {
				continue
			}
			p.Disabled = false
			p.DisabledUntil = nil
			if err := e.providerRepo.Update(ctx, p); err != nil {
				e.logger.Error("expirer: failed to re-enable provider", "id", id, "error", err)
			} else {
				e.logger.Info("expirer: auto re-enabled provider", "id", id)
			}
		}
	}
}
