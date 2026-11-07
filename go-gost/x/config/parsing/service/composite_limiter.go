package service

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	corelimiter "github.com/go-gost/core/limiter"
	connlimiter "github.com/go-gost/core/limiter/conn"
	trafficlimiter "github.com/go-gost/core/limiter/traffic"
	xtraffic "github.com/go-gost/x/limiter/traffic"
	"github.com/go-gost/x/registry"
)

func resolveTrafficLimiter(names string) trafficlimiter.TrafficLimiter {
	parts := splitLimiterNames(names)
	if len(parts) == 0 {
		return nil
	}
	if len(parts) == 1 {
		return resolveSingleTrafficLimiter(parts[0])
	}
	limiters := make([]trafficlimiter.TrafficLimiter, 0, len(parts))
	for _, part := range parts {
		if lim := resolveSingleTrafficLimiter(part); lim != nil {
			limiters = append(limiters, lim)
		}
	}
	if len(limiters) == 0 {
		return nil
	}
	if len(limiters) == 1 {
		return limiters[0]
	}
	return &compositeTrafficLimiter{limiters: limiters}
}

func resolveSingleTrafficLimiter(name string) trafficlimiter.TrafficLimiter {
	lim := registry.TrafficLimiterRegistry().Get(name)
	if lim != nil {
		return lim
	}
	if val, err := strconv.Atoi(name); err == nil && val > 0 {
		return xtraffic.NewTrafficLimiter(
			xtraffic.LimitsOption(fmt.Sprintf("%s %dB %dB", xtraffic.ServiceLimitKey, val, val)),
		)
	}
	return xtraffic.NewTrafficLimiter(
		xtraffic.LimitsOption(fmt.Sprintf("%s %s %s", xtraffic.ServiceLimitKey, name, name)),
	)
}

func resolveConnLimiter(names string) connlimiter.ConnLimiter {
	parts := splitLimiterNames(names)
	if len(parts) == 0 {
		return nil
	}
	if len(parts) == 1 {
		return registry.ConnLimiterRegistry().Get(parts[0])
	}
	limiters := make([]connlimiter.ConnLimiter, 0, len(parts))
	for _, part := range parts {
		if lim := registry.ConnLimiterRegistry().Get(part); lim != nil {
			limiters = append(limiters, lim)
		}
	}
	if len(limiters) == 0 {
		return nil
	}
	if len(limiters) == 1 {
		return limiters[0]
	}
	return &compositeConnLimiter{limiters: limiters}
}

func splitLimiterNames(names string) []string {
	parts := strings.Split(names, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

type compositeTrafficLimiter struct {
	limiters []trafficlimiter.TrafficLimiter
}

func (l *compositeTrafficLimiter) In(ctx context.Context, key string, opts ...corelimiter.Option) trafficlimiter.Limiter {
	limiters := make([]trafficlimiter.Limiter, 0, len(l.limiters))
	for _, child := range l.limiters {
		if lim := child.In(ctx, key, opts...); lim != nil {
			limiters = append(limiters, lim)
		}
	}
	return newCompositeTrafficChildLimiter(limiters)
}

func (l *compositeTrafficLimiter) Out(ctx context.Context, key string, opts ...corelimiter.Option) trafficlimiter.Limiter {
	limiters := make([]trafficlimiter.Limiter, 0, len(l.limiters))
	for _, child := range l.limiters {
		if lim := child.Out(ctx, key, opts...); lim != nil {
			limiters = append(limiters, lim)
		}
	}
	return newCompositeTrafficChildLimiter(limiters)
}

type compositeTrafficChildLimiter struct {
	limiters []trafficlimiter.Limiter
}

func newCompositeTrafficChildLimiter(limiters []trafficlimiter.Limiter) trafficlimiter.Limiter {
	if len(limiters) == 0 {
		return nil
	}
	if len(limiters) == 1 {
		return limiters[0]
	}
	sort.Slice(limiters, func(i, j int) bool {
		return limiters[i].Limit() < limiters[j].Limit()
	})
	return &compositeTrafficChildLimiter{limiters: limiters}
}

func (l *compositeTrafficChildLimiter) Wait(ctx context.Context, n int) int {
	for _, lim := range l.limiters {
		if v := lim.Wait(ctx, n); v < n {
			n = v
		}
	}
	return n
}

func (l *compositeTrafficChildLimiter) Limit() int {
	if len(l.limiters) == 0 {
		return 0
	}
	return l.limiters[0].Limit()
}

func (l *compositeTrafficChildLimiter) Set(n int) {}

type compositeConnLimiter struct {
	limiters []connlimiter.ConnLimiter
}

func (l *compositeConnLimiter) Limiter(key string) connlimiter.Limiter {
	limiters := make([]connlimiter.Limiter, 0, len(l.limiters))
	for _, child := range l.limiters {
		if lim := child.Limiter(key); lim != nil {
			limiters = append(limiters, lim)
		}
	}
	return newCompositeConnChildLimiter(limiters)
}

type compositeConnChildLimiter struct {
	limiters []connlimiter.Limiter
}

func newCompositeConnChildLimiter(limiters []connlimiter.Limiter) connlimiter.Limiter {
	if len(limiters) == 0 {
		return nil
	}
	if len(limiters) == 1 {
		return limiters[0]
	}
	sort.Slice(limiters, func(i, j int) bool {
		return limiters[i].Limit() < limiters[j].Limit()
	})
	return &compositeConnChildLimiter{limiters: limiters}
}

func (l *compositeConnChildLimiter) Allow(n int) (allowed bool) {
	var i int
	for i = range l.limiters {
		if allowed = l.limiters[i].Allow(n); !allowed {
			break
		}
	}
	if !allowed && i > 0 && n > 0 {
		for _, lim := range l.limiters[:i] {
			lim.Allow(-n)
		}
	}
	return allowed
}

func (l *compositeConnChildLimiter) Limit() int {
	if len(l.limiters) == 0 {
		return 0
	}
	return l.limiters[0].Limit()
}
