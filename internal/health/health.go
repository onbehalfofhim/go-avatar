package health

import (
	"context"
	"fmt"
	"sync"
)

type Checker struct {
	checks []namedCheck
}

type namedCheck struct {
	name  string
	check func(context.Context) error
}

func NewChecker() *Checker {
	return &Checker{}
}

func (c *Checker) Add(name string, check func(context.Context) error) {
	c.checks = append(c.checks, namedCheck{
		name:  name,
		check: check,
	})
}

type Result struct {
	OK     bool
	Checks map[string]string
}

func (c *Checker) Check(ctx context.Context) Result {
	result := Result{
		OK:     true,
		Checks: make(map[string]string, len(c.checks)),
	}

	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)

	for _, check := range c.checks {
		wg.Add(1)

		go func() {
			defer wg.Done()

			err := check.check(ctx)

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				result.OK = false
				result.Checks[check.name] = err.Error()
				return
			}

			result.Checks[check.name] = "ok"
		}()
	}

	wg.Wait()

	return result
}

func CheckError(name string, err error) error {
	return fmt.Errorf("%s: %w", name, err)
}
