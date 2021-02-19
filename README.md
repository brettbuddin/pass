# pass

[![Go
Reference](https://pkg.go.dev/badge/github.com/brettbuddin/pass.svg)](https://pkg.go.dev/github.com/brettbuddin/pass)

Pass is a library for building reverse-proxies. It wraps
[`httputil.ReverseProxy`](https://golang.org/pkg/net/http/httputil/#ReverseProxy),
and specifies routing via configuration files.

## Configuration

Routing in Pass is configured via HCL. This enables proxy routing to be
configured outside of your binary's code—such as Kubernetes `ConfigMap`
definitions.

```hcl
# Base prefix for all upstream routes. (optional)
prefix_path = "/api/v2"

# Define an upstream service called "widgets". The identifier here is just a
# human readable string to refer to this service. For instance, you could use
# this identifier when tagging metrics.
upstream "widgets" {
    # Location in the form of "scheme://hostname" to send the traffic.
    destination = "http://widgets.local" 

    # Team identifier to help keep track of who's the point of contact for a
    # particular upstream service. (optional)
    owner = "Team A <team-a@company.com>"

    # Inform the reverse-proxy to flush the response body every second. If this
    # is omitted, no flushing will be peformed. A negative value will flush
    # immediately after each write to the client. (optional)
    flush_interval_ms = 1000

    # Add an additional prefix segment (added to the root level `prefix_path`)
    # that should be stripped from outgoing requests. (optional)
    prefix_path = "/private"

    # GET `/api/v2/private/widgets` -> GET `http://widgets.local/widgets`
    # POST `/api/v2/private/widgets` -> POST `http://widgets.local/widgets`
    route {
        methods = ["GET", "POST"]
        path = "/widgets"
    }

    # GET `/api/v2/private/widgets/123` -> GET `http://widgets.local/widgets/123`
    # PUT `/api/v2/private/widgets/123` -> PUT `http://widgets.local/widgets/123`
    # DELETE `/api/v2/private/widgets/123` -> DELETE `http://widgets.local/widgets/123`
    route {
        methods = ["GET", "PUT", "DELETE"]
        path = "/widgets/{[0-9]+}"
    }
}
```

Parsing the file and mounting it in your application:

```go
m, err := pass.LoadManifest("./manifest.hcl", nil)
if err != nil {
	return err
}

// Register the manifest's routes with a router.
proxy, err := pass.New(m)
if err != nil {
	return err
}
```

You can pass an optional `hcl.EvalContext` to specify variables and functions as
part of the HCL parsing.

```go
ectx := &hcl.EvalContext{
	Variables: map[string]cty.Value{
		"namespace": cty.StringVal("example"),
	},
}

// The variable "namespace" will be available in this configuration file.
m, err := pass.LoadManifest("./manifest.hcl", ectx)
if err != nil {
	return err
}
```

## Examples

Check out the [example/](example) directory for usage examples in code.
