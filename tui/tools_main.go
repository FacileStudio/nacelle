package main

import (
	"fmt"

	"github.com/FacileStudio/nacelle"
	"github.com/FacileStudio/nacelle/tools"
)

// localTools opens the file/search/command tools and, when asked, adds
// mycelium's and the web search — the caller owns closing the returned Set.
//
// The two internet tools are built together in webTools, which is also where
// the reason WebSearch's error goes back unwrapped is written down.
//
// Every failure after tools.New succeeds closes the Set on the way out.
// Without that it is dropped on the floor: the caller's own defer only ever
// sees the nil this returns on an error, so the *os.Root behind it — a real
// descriptor — outlives the only reference to it. The process exits moments
// later today and nothing notices, which is the kind of leak that becomes
// real the first time something rebuilds a tool set without restarting.
//
// The handle is held in a plain local rather than in the returned one, and
// that is load-bearing rather than style. A named result is assigned by the
// return statement *before* any deferred function runs, so with the Set named
// there, `return nil, nil, err` had already overwritten it — the deferred
// close then panicked on a nil receiver instead of releasing anything. The
// error paths still hand the caller nil, which is what it wants; only the
// closing needs a name the returns cannot reach.
func localTools(config Config) (_ *tools.Set, local []nacelle.Tool, err error) {
	opened, err := tools.New(tools.Config{Root: config.Root, AllowBash: *config.Bash})
	if err != nil {
		return nil, nil, fmt.Errorf("opening %s: %w", config.Root, err)
	}

	defer func() {
		if err != nil {
			_ = opened.Close()
		}
	}()

	local, err = opened.Tools()
	if err != nil {
		return nil, nil, fmt.Errorf("building the tool set: %w", err)
	}
	if *config.Mycelium {
		var myceliumTools []nacelle.Tool
		if myceliumTools, err = tools.Mycelium(); err != nil {
			return nil, nil, fmt.Errorf("building mycelium's tools: %w", err)
		}
		local = append(local, myceliumTools...)
	}

	var reaching []nacelle.Tool
	if reaching, err = webTools(config); err != nil {
		return nil, nil, err
	}
	local = append(local, reaching...)

	return opened, local, nil
}
