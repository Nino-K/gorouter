package handlers

type key string

// ProxyResponseWriterCtxKey is a key used to store the proxy response writer
// in the request context
const ProxyResponseWriterCtxKey key = "ProxyResponseWriter"

// RouteServiceURLCtxKey is a key used to store the route service url
// to indicate that this request is destined for a route service
const RouteServiceURLCtxKey key = "RouteServiceURL"

// InternalRouteServiceCtxKey is a key used to mark requests
// to indicate the route service is an app running on CF
const InternalRouteServiceCtxKey key = "InternalRouteService"
