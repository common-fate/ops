package ops

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"strings"

	"github.com/common-fate/ops/protocol"
	"github.com/common-fate/ops/servicedef"
	"github.com/common-fate/ops/tunnel"
	"github.com/invopop/jsonschema"
	"github.com/quic-go/quic-go"
)

type ResourceHandler[R any] struct {
	operations map[string]any
}

type Registry struct {
	services  []any
	resources []any
}

type function struct {
	method    reflect.Value
	inputType *reflect.Type
}

type Handler struct {
	// map service -> operation -> Go function
	routes map[string]map[string]function

	defs servicedef.Definitions
}

func New() *Registry {
	return &Registry{}
}

type ResourceSchema[R any] struct{}

func (r ResourceSchema[R]) resourceType() {

}

// Use ops.NewResource() to construct a resource.
type Resource interface {
	resourceType()
}

func NewResource[R any](loader ResourceLoader[R]) *ResourceSchema[R] {
	r := new(ResourceSchema[R])
	return r
}

type ResourceLoader[R any] interface {
	Load(ctx context.Context, id string) (*R, error)
}

type ServiceMetadata struct {
	ID                string
	DisplayName       string
	Description       string
	OperationMetadata map[string]OperationMetadata
}

type OperationMetadata struct {
	Description string
}

type ServiceWithMetadata interface {
	Metadata() ServiceMetadata
}

func (h *Registry) Register(service any) {
	h.services = append(h.services, service)
}

// Register a new resource.
//
// Example:
//
//	h.RegisterResource(ops.NewResource(customer))
func (h *Registry) RegisterResource(r Resource) {
	h.resources = append(h.resources, r)
}

func (h *Handler) ServiceDefinitions() servicedef.Definitions {
	return h.defs
}

func (h *Handler) Call(ctx context.Context, service string, operation string, input json.RawMessage) ([]byte, error) {
	svcroutes, ok := h.routes[service]
	if !ok {
		return nil, fmt.Errorf("service %s not found", service)
	}

	function, ok := svcroutes[operation]
	if !ok {
		return nil, fmt.Errorf("operation %s not found for service %s", operation, service)
	}

	var args []reflect.Value

	args = append(args, reflect.ValueOf(ctx)) // TODO: ctx should not always be required

	if function.inputType != nil {
		v := reflect.New(*function.inputType)
		valInt := v.Interface()

		err := json.Unmarshal(input, &valInt)
		if err != nil {
			return nil, fmt.Errorf("error unmarshalling input: %w", err)
		}
		args = append(args, reflect.ValueOf(valInt).Elem())
	}

	output := function.method.Call(args)
	result := output[0] // TODO: output should not always be required
	msgValue := result.Interface()

	return json.Marshal(msgValue)
}

func (r *Registry) Build() (*Handler, error) {
	h := Handler{
		routes: map[string]map[string]function{},
	}

	for _, svc := range r.services {
		v := reflect.ValueOf(svc)

		if v.Kind() != reflect.Pointer {
			return nil, fmt.Errorf("received a struct that wasn't a pointer for %T: ensure that you call Register() with the address of the struct, e.g. Register(&MyService{})", svc)
		}

		tt := reflect.TypeOf(svc)

		sdef := servicedef.Service{
			ID: v.Elem().Type().Name(),
		}

		var meta ServiceMetadata

		if metasrv, ok := svc.(ServiceWithMetadata); ok {
			meta = metasrv.Metadata()

			sdef = servicedef.Service{
				ID:          meta.ID,
				Name:        meta.DisplayName,
				Description: meta.Description,
			}
		}

		_, exists := h.routes[sdef.ID]
		if exists {
			return nil, fmt.Errorf("a service with ID '%s' has already been registered, please rename the service or remove the second registration (you can update the ID by setting it in Metadata())", sdef.ID)
		}

		routeMap := map[string]function{}

		for i := 0; i < tt.NumMethod(); i++ {
			method := tt.Method(i)

			if method.Name == "Metadata" {
				continue
			}

			methodValue := v.Method(i)

			opMeta := meta.OperationMetadata[method.Name]

			op := servicedef.Operation{
				ID:          method.Name,
				Description: opMeta.Description,
			}

			extract, err := extractMethods(method.Func)
			if err != nil {
				slog.Error("error extracting method", "error", err)
			}
			if extract.InputSchema != nil {
				op.RequestBody = &servicedef.RootSchema{
					Schema: *extract.InputSchema,
				}
			}

			parsed, ok := parseMethod(method, methodValue, meta)
			if ok {
				routeMap[parsed.operation.ID] = function{
					method:    methodValue,
					inputType: extract.InputType,
				}
				sdef.Operations = append(sdef.Operations, op)
			}
		}

		h.routes[sdef.ID] = routeMap
		h.defs.Services = append(h.defs.Services, sdef)
	}

	return &h, nil
}

type parseMethodResult struct {
	function  function
	operation servicedef.Operation
}

func parseMethod(method reflect.Method, methodValue reflect.Value, meta ServiceMetadata) (parseMethodResult, bool) {
	if method.Name == "Metadata" {
		return parseMethodResult{}, false
	}

	opMeta := meta.OperationMetadata[method.Name]

	op := servicedef.Operation{
		ID:          method.Name,
		Description: opMeta.Description,
	}

	extract, err := extractMethods(method.Func)
	if err != nil {
		slog.Error("error extracting method", "error", err)
	}
	if extract.InputSchema != nil {
		op.RequestBody = &servicedef.RootSchema{
			Schema: *extract.InputSchema,
		}
	}

	res := parseMethodResult{
		function: function{
			method:    methodValue,
			inputType: extract.InputType,
		},
		operation: op,
	}

	return res, true
}

type extractMethodsResult struct {
	InputSchema *jsonschema.Schema
	InputType   *reflect.Type
}

func extractMethods(f reflect.Value) (extractMethodsResult, error) {
	funcType := f.Type()
	var res extractMethodsResult

	for i := 1; i < funcType.NumIn(); i++ {
		t := funcType.In(i)
		v := reflect.New(t)

		interf := v.Interface()
		// should be possible to relax this in future,
		// for example if the function does not do anything
		// async and doesn't take a context.
		_, isCtx := interf.(*context.Context)
		if !isCtx && i == 1 {
			return res, fmt.Errorf("first arg was not context.Context, got %T", interf)
		}

		if i == 2 {
			res.InputSchema = jsonschema.Reflect(v.Interface())
			res.InputType = &t

			return res, nil
		}
	}
	return res, nil
}

type StartOpts struct {
	Namespace string
	// TLSConfig allows the tunnel TLS
	// config to be optionally overridden.
	TLSConfig         *tls.Config
	QuicConfig        *quic.Config
	OnConnectionReady func(protocol.RegisterListenerResponse)
	Logger            *slog.Logger
	Addr              string
}

func (r *Registry) Start(ctx context.Context, opts StartOpts) error {
	h, err := r.Build()
	if err != nil {
		return err
	}

	server := tunnel.Tunnel{
		Namespace:         opts.Namespace,
		TLSConfig:         opts.TLSConfig,
		Logger:            opts.Logger,
		QuicConfig:        opts.QuicConfig,
		OnConnectionReady: opts.OnConnectionReady,
		Handler:           h,
	}

	return server.DialAndServe(ctx, opts.Addr)
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" && r.URL.Path == "/.lightwave/operations" {
		err := json.NewEncoder(w).Encode(h.defs)
		if err != nil {
			slog.Error("error marshalling operations", "error", err)
			_, _ = w.Write([]byte(err.Error()))
		}
		return
	}

	if r.Method != "POST" {
		// POST-only protocol
		w.WriteHeader(http.StatusMethodNotAllowed)
		w.Write([]byte(http.StatusText(http.StatusMethodNotAllowed)))
		return
	}

	urlPath := strings.TrimPrefix(r.URL.Path, "/")
	parts := strings.Split(urlPath, "/")
	// expect path to be /service/method
	if len(parts) != 2 {
		w.WriteHeader(http.StatusNotFound)
		msg := fmt.Sprintf("invalid path: %s", r.URL.Path)
		w.Write([]byte(msg))
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(err.Error()))
		return
	}

	service := parts[0]
	op := parts[1]

	res, err := h.Call(r.Context(), service, op, body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(err.Error()))
		return
	}

	w.Write(res)
}
