package handlers

import (
	"errors"
	"net/http"

	"golang.org/x/net/context"

	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/registry"
	"code.cloudfoundry.org/gorouter/routeservice"
	"github.com/uber-go/zap"
	"github.com/urfave/negroni"

	"code.cloudfoundry.org/gorouter/route"
)

type routeService struct {
	routeRegistry  registry.Registry
	rsReverseProxy RouteServiceProxy
	config         *routeservice.RouteServiceConfig
	logger         logger.Logger
}

//go:generate counterfeiter -o fakes/fake_route_service_proxy.go . RouteServiceProxy
type RouteServiceProxy interface {
	ServeHTTP(responseWriter http.ResponseWriter, request *http.Request)
}

// TODO: the new routeService reverseProxy will get injected into this constructor along with the registery
// we should have a handler that triggers reverseProxy.ServeHttp if doing an external route service request
// routeService reverseProxy will be configured with a routeService round tripper
// If using an internal route service, must change context to have the pool for the route service

// NewRouteService creates a handler responsible for handling route services
func NewRouteService(routeRegistry registry.Registry, rsReverseProxy RouteServiceProxy, config *routeservice.RouteServiceConfig, logger logger.Logger) negroni.Handler {
	return &routeService{
		routeRegistry:  routeRegistry,
		rsReverseProxy: rsReverseProxy,
		config:         config,
		logger:         logger,
	}
}

func (r *routeService) ServeHTTP(rw http.ResponseWriter, req *http.Request, next http.HandlerFunc) {
	alr := req.Context().Value("AccessLogRecord")

	rp := req.Context().Value("RoutePool")
	if rp == nil {
		r.logger.Error("RoutePool not set on context", zap.Error(errors.New("failed-to-access-RoutePool")))
		http.Error(rw, "RoutePool not set on context", http.StatusBadGateway)
		return
	}
	routePool := rp.(*route.Pool)

	routeServiceUrl := routePool.RouteServiceUrl()
	// Attempted to use a route service when it is not supported
	if routeServiceUrl != "" && !r.config.RouteServiceEnabled() {
		r.logger.Info("route-service-unsupported")

		rw.Header().Set("X-Cf-RouterError", "route_service_unsupported")
		writeStatus(
			rw,
			http.StatusBadGateway,
			"Support for route services is disabled.",
			alr,
			r.logger,
		)
		return
	}

	var routeServiceArgs routeservice.RouteServiceRequest
	if routeServiceUrl != "" {
		rsSignature := req.Header.Get(routeservice.RouteServiceSignature)

		var recommendedScheme string

		if r.config.RouteServiceRecommendHttps() {
			recommendedScheme = "https"
		} else {
			recommendedScheme = "http"
		}

		forwardedURLRaw := recommendedScheme + "://" + hostWithoutPort(req) + req.RequestURI
		if hasBeenToRouteService(routeServiceUrl, rsSignature) {
			// A request from a route service destined for a backend instances
			routeServiceArgs.URLString = routeServiceUrl
			err := r.config.ValidateSignature(&req.Header, forwardedURLRaw)
			if err != nil {
				r.logger.Error("signature-validation-failed", zap.Error(err))

				writeStatus(
					rw,
					http.StatusBadRequest,
					"Failed to validate Route Service Signature",
					alr,
					r.logger,
				)
				return
			}
			// Remove the headers since the backend should not see it
			req.Header.Del(routeservice.RouteServiceSignature)
			req.Header.Del(routeservice.RouteServiceMetadata)
			req.Header.Del(routeservice.RouteServiceForwardedURL)
		} else {
			var err error
			// should not hardcode http, will be addressed by #100982038
			routeServiceArgs, err = r.config.Request(routeServiceUrl, forwardedURLRaw)
			if err != nil {
				r.logger.Error("route-service-failed", zap.Error(err))

				writeStatus(
					rw,
					http.StatusInternalServerError,
					"Route service request failed.",
					alr,
					r.logger,
				)
				return
			}
			req.Header.Set(routeservice.RouteServiceSignature, routeServiceArgs.Signature)
			req.Header.Set(routeservice.RouteServiceMetadata, routeServiceArgs.Metadata)
			req.Header.Set(routeservice.RouteServiceForwardedURL, routeServiceArgs.ForwardedURL)

			req = req.WithContext(context.WithValue(req.Context(), RouteServiceURLCtxKey, routeServiceArgs.ParsedUrl))

			routeURI := route.Uri(routeServiceArgs.ParsedUrl.Host + routeServiceArgs.ParsedUrl.Path)
			rsPool := r.routeRegistry.Lookup(routeURI)

			req = req.WithContext(context.WithValue(req.Context(), "RoutePool", rsPool))

			if rsPool == nil || rsPool.IsEmpty() {
				r.rsReverseProxy.ServeHTTP(rw, req)
				return
			}
		}
	}
	next(rw, req)
}

func hasBeenToRouteService(rsUrl, sigHeader string) bool {
	return sigHeader != "" && rsUrl != ""
}
