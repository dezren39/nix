package config

import (
	"github.com/dezren39/mcpx/internal/settings"
)

// ApplyPoolSettings folds the resolved pool knobs into the file-level pool
// defaults.
//
// Before this existed, `pool.max` and its six neighbours were honoured from a
// configuration file and ignored everywhere else: the file layer reached
// Config.Pool through the JSON decoder, while MCPX_POOL_MAX and --pool-max
// landed only in the resolved Set, which nothing on this path read. The
// registry promises all three, so the two paths have to meet somewhere, and
// the honest place is the layer the file wrote to.
//
// Only a value that came from above the default layer is written. The default
// layer is already what Resolve falls back to, so copying it in would be a
// no-op at best; at worst it would turn "the user said nothing" into "the
// user said four", which matters because Resolve distinguishes them when a
// server sets its own value.
//
// Per-server mcpx.max still wins, because this writes the layer beneath it.
func ApplyPoolSettings(c *Config, set *settings.Set) {
	if c == nil || set == nil {
		return
	}
	if set.Given("pool.sharing") {
		c.Pool.Sharing = Sharing(set.String("pool.sharing"))
	}
	if set.Given("pool.scope") {
		c.Pool.Scope = Scope(set.String("pool.scope"))
	}
	if set.Given("pool.max") {
		c.Pool.Max = set.Int("pool.max")
	}
	if set.Given("pool.min") {
		c.Pool.Min = set.Int("pool.min")
	}
	if set.Given("pool.idleTimeout") {
		c.Pool.IdleTimeout = set.String("pool.idleTimeout")
	}
	if set.Given("pool.callTimeout") {
		c.Pool.CallTimeout = set.String("pool.callTimeout")
	}
	if set.Given("pool.startTimeout") {
		c.Pool.StartTimeout = set.String("pool.startTimeout")
	}
}
