# A2A agents

**English** · [Русский](ru/a2a.md)

MikroLLM proxies [Agent2Agent](https://github.com/a2aproject/A2A) JSON-RPC the same way it proxies MCP. An agent talks to this gateway. The gateway calls the upstream URL and adds the saved bearer. The hub stores the agent name only.

## Local agent

Admin **Agents** (`Агенты`): name, JSON-RPC URL, optional bearer. The URL is `http` or `https` with no userinfo. The name is a slug, `^[a-z0-9][a-z0-9_-]{0,39}$`.

| Method | Path | What happens |
|---|---|---|
| GET | `/a2a/u/{name}/.well-known/agent-card.json` | Upstream card, or a small card if the upstream has none. `url` is rewritten to this gateway |
| POST, DELETE | `/a2a/u/{name}` | JSON-RPC on the saved URL |
| POST, GET, DELETE | `/a2a/u/{name}/{path}` | Same URL plus a relative path. `..` is rejected |

Auth is the gateway MCP bearer (`mcp-…` or the admin password). That header is not forwarded. An empty token field on save keeps the stored secret. The token is not rendered in admin HTML.

## Network

On **Status**, **Hub network member** reveals **Agent access** (`Доступ к агентам`). It is separate from **MCP access** and from model aliases. On the Agents tab, **In network** (`В сеть`) picks which agents that flag publishes. Turn either one off and the hub list is cleared. A disabled checkbox does not clear a row that was already shared. The share window is the same schedule as aliases. Agent jobs use the chat concurrency number.

The catalog and the hub UI show the name when the node shares agents or MCP.

| Method | Path | What happens |
|---|---|---|
| GET | `/a2a/u/{node}/{name}/.well-known/agent-card.json` | Neighbor card. `url` becomes this gateway, so the next call stays here |
| POST | `/a2a/u/{node}/{name}` | Hub `POST /v1/relay/{node}/a2a/{name}`. 404 if that name was not announced |

`{node}` is the hub id (`n` plus 32 hex digits). The owner returns the raw card. The caller rewrites it. Body limit is 1 MiB.
