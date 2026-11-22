package lifecycle

type Closers struct {
	closers []func()
}

func (c *Closers) Add(close func()) {
	c.closers = append(c.closers, close)
}

func (c *Closers) Close() {
	for i := len(c.closers) - 1; i >= 0; i-- {
		c.closers[i]()
	}
	c.closers = nil
}
