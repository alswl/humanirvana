// Package reviewcombination exposes one deterministic native Huma operation
// with an optional, independent Nirvana legacy route for human inspection.
package reviewcombination

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/alswl/humanirvana"
	"github.com/caicloud/nirvana/definition"
	"github.com/caicloud/nirvana/service"
	"github.com/caicloud/nirvana/service/rest"
	"github.com/danielgtaylor/huma/v2"
)

type EstimateInput struct {
	Team     string `path:"team" doc:"Owning team identifier"`
	Currency string `header:"X-Currency" enum:"USD,EUR" doc:"Estimate currency"`
	Discount int    `query:"discount" minimum:"0" maximum:"50" default:"10" doc:"Discount percentage"`
	Body     struct {
		Items []struct {
			Name     string `json:"name" minLength:"1"`
			UnitCost int    `json:"unit_cost" minimum:"1"`
			Quantity int    `json:"quantity" minimum:"1" maximum:"20"`
		} `json:"items" minItems:"1"`
	} `json:"body"`
}

type EstimateOutput struct {
	Status int
	Team   string `header:"X-Team"`
	Body   struct {
		Team      string `json:"team"`
		Currency  string `json:"currency"`
		Subtotal  int    `json:"subtotal"`
		Discount  int    `json:"discount_percent"`
		Total     int    `json:"total"`
		LineCount int    `json:"line_count"`
	} `json:"body"`
}

func calculate(_ context.Context, input *EstimateInput) (*EstimateOutput, error) {
	if strings.TrimSpace(input.Team) == "" {
		return nil, huma.Error400BadRequest("team must not be empty")
	}
	subtotal := 0
	for _, item := range input.Body.Items {
		subtotal += item.UnitCost * item.Quantity
	}
	out := &EstimateOutput{Status: http.StatusCreated, Team: input.Team}
	out.Body.Team, out.Body.Currency = input.Team, input.Currency
	out.Body.Subtotal, out.Body.Discount = subtotal, input.Discount
	out.Body.Total = subtotal * (100 - input.Discount) / 100
	out.Body.LineCount = len(input.Body.Items)
	return out, nil
}

func newAPI(mode string) (http.Handler, error) {
	builder := rest.NewBuilder()
	builder.SetModifier(service.ConsumeNoneForHTTPGet())
	adapter := humanirvana.NewAdapter(builder)
	api := huma.NewAPI(huma.DefaultConfig("Human review combination", "1.0.0"), adapter)
	huma.Register(api, huma.Operation{
		OperationID:   "createEstimate",
		Method:        http.MethodPost,
		Path:          "/review/estimates/{team}",
		Summary:       "Create a deterministic estimate",
		DefaultStatus: http.StatusCreated,
	}, calculate)

	type routeInput struct {
		ID string `path:"id"`
	}
	type routeOutput struct {
		Body struct {
			ID    string `json:"id"`
			Route string `json:"route"`
		}
	}
	huma.Register[routeInput, routeOutput](api, huma.Operation{Method: http.MethodGet, Path: "/review/routes/{id}", OperationID: "reviewParameter"}, func(_ context.Context, in *routeInput) (*routeOutput, error) {
		out := &routeOutput{}
		out.Body.ID, out.Body.Route = in.ID, "parameter"
		return out, nil
	})
	huma.Register[struct{}, routeOutput](api, huma.Operation{Method: http.MethodGet, Path: "/review/routes/static", OperationID: "reviewStatic"}, func(context.Context, *struct{}) (*routeOutput, error) {
		out := &routeOutput{}
		out.Body.ID, out.Body.Route = "static", "static"
		return out, nil
	})
	huma.Register[routeInput, routeOutput](api, huma.Operation{Method: http.MethodGet, Path: "/review/guarded/{id}", OperationID: "reviewGuarded", Middlewares: huma.Middlewares{func(ctx huma.Context, next func(huma.Context)) {
		ctx.SetHeader("X-Review-Guard", "checked")
		if ctx.Header("X-Review-Token") != "allow" {
			_ = huma.WriteErr(api, ctx, http.StatusForbidden, "review token required")
			return
		}
		next(ctx)
	}}}, func(_ context.Context, in *routeInput) (*routeOutput, error) {
		out := &routeOutput{}
		out.Body.ID, out.Body.Route = in.ID, "guarded"
		return out, nil
	})
	if mode == "mixed" {
		legacy := definition.Descriptor{Path: "/legacy/status", Consumes: []string{definition.MIMEAll}, Definitions: []definition.Definition{{
			Method: definition.Get,
			Function: func(context.Context) (map[string]string, error) {
				return map[string]string{"status": "legacy-route"}, nil
			},
			Results:    definition.DataErrorResults("LegacyStatus"),
			Parameters: []definition.Parameter{{Source: definition.Prefab, Name: "context"}},
			Consumes:   []string{definition.MIMEJSON}, Produces: []string{definition.MIMEJSON}, ErrorProduces: []string{definition.MIMEJSON},
		}}}
		if err := humanirvana.RegisterLegacy(builder, []definition.Descriptor{legacy}); err != nil {
			return nil, err
		}
	} else if mode != "native" {
		return nil, fmt.Errorf("unknown mode %q (want native or mixed)", mode)
	}
	service, err := builder.Build()
	if err != nil {
		return nil, err
	}
	if err := adapter.Bind(service); err != nil {
		return nil, err
	}
	return adapter, nil
}

// Handler returns a fresh router for the requested mode.
func Handler(mode string) (http.Handler, error) { return newAPI(mode) }
