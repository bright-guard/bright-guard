package mcp

// The legacy MCP "SSE" transport (GET an event stream + POST replies on a
// separate endpoint) is intentionally NOT implemented in this round. Most
// real-world MCP servers we target speak the newer Streamable HTTP transport,
// which already negotiates SSE responses opportunistically (see
// transport_streamable.go).
//
// For now, when a connection is created with transport="sse" we fall back to
// the streamable-http transport. If we encounter a server that only speaks the
// legacy SSE protocol we'll add a real implementation here.
//
// TODO: implement legacy SSE transport (GET /sse for events, POST /messages
// for client→server).
