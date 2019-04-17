package scheduler

import (
	"time"

	"github.com/aryszka/jobstack"
	log "github.com/sirupsen/logrus"
	"github.com/zalando/skipper/routing"
)

// note: Config must stay comparable because it is used to detect changes in route specific LIFO config

type Config struct {
	MaxConcurrency int
	MaxStackSize   int
	Timeout        time.Duration
}

type Stack struct {
	stack  *jobstack.Stack
	config Config
}

type Registry struct {
	global      *Stack
	groupConfig map[string]Config
	stacks      map[string]*Stack
}

type LIFOFilter interface {
	SetStack(*Stack)
}

type ConfiguredFilter interface {
	LIFOFilter
	Config() Config
}

type GroupFilter interface {
	LIFOFilter
	GroupName() string
}

func newStack(c Config) *Stack {
	return &Stack{
		config: c,
		stack: jobstack.With(jobstack.Options{
			MaxConcurrency: c.MaxConcurrency,
			MaxStackSize:   c.MaxStackSize,
			Timeout:        c.Timeout,
		}),
	}
}

func (s *Stack) Ready() (done func(), err error) {
	return s.stack.Ready()
}

func (s *Stack) close() {
	s.stack.Close()
}

func NewRegistry(global Config, groups map[string]Config) *Registry {
	return &Registry{
		global:      newStack(global),
		groupConfig: groups,
		stacks:      make(map[string]*Stack),
	}
}

func (r *Registry) get(name string) (s *Stack, ok bool) {
	s, ok = r.stacks[name]
	return
}

func (r *Registry) set(name string, s *Stack) {
	r.stacks[name] = s
}

// Do implements routing.PostProcessor and sets the stack for the scheduler filters.
//
// It preserves the existing stack when available.
func (r *Registry) Do(routes []*routing.Route) []*routing.Route {
	rr := make([]*routing.Route, len(routes))
	for i, ri := range routes {
		foundStack := false
		rr[i] = ri

		for _, fi := range ri.Filters {
			cf, ok := fi.Filter.(ConfiguredFilter)
			if ok {
				c := cf.Config()
				s, ok := r.get(ri.Id)
				if !ok {
					s = newStack(c)
					r.set(ri.Id, s)
				} else if c != s.config {
					s.close()
					s = newStack(c)
					r.set(ri.Id, s)
				}

				cf.SetStack(s)
				foundStack = true
			}

			nf, ok := fi.Filter.(GroupFilter)
			if ok {
				n := nf.GroupName()
				s, ok := r.get(n)
				if !ok {
					s = newStack(r.groupConfig[n])
					r.set(n, s)
				}

				nf.SetStack(s)
				foundStack = true
			}
		}
		// for testing purposes to add some kind of "global"
		if !foundStack {
			log.Errorf("Failed to find a stack filter")
		}
		if !foundStack && r.global != nil {
			// fs, err := eskip.ParseFilters(`lifo(1,2,"10s")`)
			// if err != nil {
			// 	log.Errorf("Failed to create fallback scheduler: %v", err)
			// }

			// ef := fs[0] // eskip.Filter
			// f := filters.Filter(ef)
			// rf := routes.RouteFilter(f) // needs filters.Filter -> routes.RouteFilter undefined (type []*routing.Route has no field or method RouteFilter)
			// rf.Name = f.Name
			// rf.Index = 5
			// ri.Filters = append(ri.Filters, rf...)
		}
	}

	return rr
}

func (r *Registry) Close() {
	for _, s := range r.stacks {
		s.close()
	}

	r.global.close()
}
