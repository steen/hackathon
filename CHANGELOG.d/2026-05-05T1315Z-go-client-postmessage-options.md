### packages/go-client — `PostMessage` takes `PostMessageOptions`

`Client.PostMessage` was the lone deviation from the `ctx, requiredPositional..., opts struct` shape used by `ListMessages` and `Watch`. It now matches: the previous `body string` parameter is folded into a new `PostMessageOptions{Body: string}` value. Future tunables (e.g. idempotency keys) can land on the struct without another breaking signature change.

Pre-1.0 breaking change. The CLI's `chatd send` consumer is updated in the same commit; external callers (none known in-repo) should adapt the call to:

```go
client.PostMessage(ctx, channelID, goclient.PostMessageOptions{Body: body})
```

Closes #602.
