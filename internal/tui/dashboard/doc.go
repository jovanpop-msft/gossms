// Package dashboard lays out the Activity Monitor's dashboards: History, Sample,
// TempDB, and — on an Azure engine edition — Instance.
//
// It is a leaf of the application layer, like planview and sqlparse: it depends
// on tuikit and the standard library, never on internal/tui, and knows nothing
// about SQL Server, connections or collection. Its input is a plain view model
// (view.go), so the Activity Monitor panel and cmd/amdemo draw the same
// dashboards.
//
// A dashboard draws into a fixed-size canvas, not the terminal: a full dashboard
// exceeds most terminals, so the caller renders once at canvas size and scrolls
// a viewport. The *CanvasW/*CanvasH constants are those sizes.
//
// InstanceView differs: the others are built from samples the *caller*
// collected and differenced, but InstanceView comes from a history the server
// already aggregated into fixed windows, so its Interval is the server's window,
// not the caller's collection rate. Scaling its time axis by the refresh rate
// would mislabel every column.
package dashboard
