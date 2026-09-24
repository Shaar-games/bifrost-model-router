package main

import (
	"fmt"
	"sync/atomic"

	"github.com/applyinnovations/bifrost-model-router/internal/catalog"
	"github.com/applyinnovations/bifrost-model-router/internal/editorial"
	"github.com/applyinnovations/bifrost-model-router/internal/routerplugin"
	"github.com/maximhq/bifrost/core/schemas"
)

var defaultPlugin atomic.Pointer[routerplugin.Plugin]
var metadataLookup catalog.MetadataLookup = editorial.NewOpenRouterResolver().LookupMetadata

func Init(raw any) error {
	plugin, err := routerplugin.NewWithMetadataLookup(raw, metadataLookup)
	if err != nil {
		return err
	}
	defaultPlugin.Store(plugin)
	return nil
}

func GetName() string { return routerplugin.Name }

func Cleanup() error {
	if plugin := defaultPlugin.Load(); plugin != nil {
		return plugin.Cleanup()
	}
	return nil
}

func plugin() (*routerplugin.Plugin, error) {
	plugin := defaultPlugin.Load()
	if plugin == nil {
		return nil, fmt.Errorf("%s is not initialized", routerplugin.Name)
	}
	return plugin, nil
}

func HTTPTransportPreAuthHook(ctx *schemas.BifrostContext, req *schemas.HTTPRequest) (*schemas.HTTPResponse, error) {
	plugin, err := plugin()
	if err != nil {
		return nil, err
	}
	return plugin.HTTPTransportPreAuthHook(ctx, req)
}

func HTTPTransportPreHook(ctx *schemas.BifrostContext, req *schemas.HTTPRequest) (*schemas.HTTPResponse, error) {
	plugin, err := plugin()
	if err != nil {
		return nil, err
	}
	return plugin.HTTPTransportPreHook(ctx, req)
}

func HTTPTransportPostHook(ctx *schemas.BifrostContext, req *schemas.HTTPRequest, resp *schemas.HTTPResponse) error {
	plugin, err := plugin()
	if err != nil {
		return err
	}
	return plugin.HTTPTransportPostHook(ctx, req, resp)
}

func HTTPTransportStreamChunkHook(ctx *schemas.BifrostContext, req *schemas.HTTPRequest, chunk *schemas.BifrostStreamChunk) (*schemas.BifrostStreamChunk, error) {
	plugin, err := plugin()
	if err != nil {
		return nil, err
	}
	return plugin.HTTPTransportStreamChunkHook(ctx, req, chunk)
}

func PreRequestHook(ctx *schemas.BifrostContext, req *schemas.BifrostRequest) error {
	plugin, err := plugin()
	if err != nil {
		return err
	}
	return plugin.PreRequestHook(ctx, req)
}

func PreLLMHook(ctx *schemas.BifrostContext, req *schemas.BifrostRequest) (*schemas.BifrostRequest, *schemas.LLMPluginShortCircuit, error) {
	plugin, err := plugin()
	if err != nil {
		return req, nil, err
	}
	return plugin.PreLLMHook(ctx, req)
}

func PostLLMHook(ctx *schemas.BifrostContext, resp *schemas.BifrostResponse, bifrostErr *schemas.BifrostError) (*schemas.BifrostResponse, *schemas.BifrostError, error) {
	plugin, err := plugin()
	if err != nil {
		return resp, bifrostErr, err
	}
	return plugin.PostLLMHook(ctx, resp, bifrostErr)
}
