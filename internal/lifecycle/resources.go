package lifecycle

type Resources struct {
	closers []func()
	checks  []Check
}

func (r *Resources) Add(close func(), check Check) {
	r.closers = append(r.closers, close)
	if check != nil {
		r.checks = append(r.checks, check)
	}
}

func (r *Resources) Checks() []Check {
	return r.checks
}

func (r *Resources) Close() {
	for i := len(r.closers) - 1; i >= 0; i-- {
		r.closers[i]()
	}
	r.closers = nil
}
