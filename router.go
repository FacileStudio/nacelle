package nacelle

import "encoding/json"

// Router is a Tool that dispatches to another one, such as a catalog's
// call_tool naming the tool to run inside its input.
//
// The loop routes a call before the hooks and Approve see it, so a policy,
// an approval prompt and the result event all name the tool that actually
// runs, with the input that tool reads. Route returns that tool and its
// input, or the error the model reads when the input names nothing.
type Router interface {
	Route(input json.RawMessage) (Tool, json.RawMessage, error)
}

// target is a call resolved to the tool that will run it, or to the error
// the model reads instead.
type target struct {
	tool Tool
	call Call
	err  error
}

// resolve finds the tool a call names, following a Router to the tool it
// routes to.
func (s *sink) resolve(call Call) target {
	tool, known := s.tools[call.Name]
	if !known {
		return target{call: call, err: errNoTool(call.Name)}
	}
	router, routes := tool.(Router)
	if !routes {
		return target{tool: tool, call: call}
	}
	inner, input, err := router.Route(call.Input)
	if err != nil {
		return target{call: call, err: err}
	}
	call.Name, call.Input = inner.Spec().Name, input
	return target{tool: inner, call: call}
}
