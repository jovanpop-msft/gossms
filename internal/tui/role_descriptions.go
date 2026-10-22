package tui

import (
	"context"

	gosmo "github.com/radix29/gosmo"
	"github.com/radix29/gossms/internal/db"
)

// fixedRoleDescriptions gives each fixed database role a short blurb for Database
// User Properties' Membership page, matching SSMS's descriptions. User-defined
// roles have no entry (blank).
var fixedRoleDescriptions = map[string]string{
	"db_owner":          "Full control over the database",
	"db_accessadmin":    "Add or remove access for logins",
	"db_securityadmin":  "Manage role membership and permissions",
	"db_ddladmin":       "Run any DDL command",
	"db_backupoperator": "Back up the database",
	"db_datareader":     "Read all data from all user tables",
	"db_datawriter":     "Add, delete, or change data in all user tables",
	"db_denydatareader": "Deny SELECT on all user tables",
	"db_denydatawriter": "Deny INSERT/UPDATE/DELETE on all user tables",
}

// fixedServerRoleDescriptions and fixedServerRoleImpact give each fixed server
// role a short blurb and an impact level for the Server Roles pages of Login
// Properties and New Login. The ##MS_*## roles (SQL Server 2022+) hold their
// permissions implicitly — sys.server_permissions lists none for them — so
// their entries are written from what fn_my_permissions reports for a member
// (read live on 17). User-defined roles are rated from their grants by
// serverRoleRatings.
var fixedServerRoleDescriptions = map[string]string{
	"sysadmin":      "Full control over the SQL Server instance",
	"securityadmin": "Manage logins and their properties",
	"serveradmin":   "Configure server-wide settings",
	"setupadmin":    "Add and remove linked servers",
	"processadmin":  "Manage processes running in SQL Server",
	"diskadmin":     "Manage disk files",
	"dbcreator":     "Create, alter, drop, and restore databases",
	"bulkadmin":     "Run BULK INSERT statements",
	"public":        "Every login is a member",

	"##MS_DatabaseConnector##":            "Connect to any database",
	"##MS_DatabaseManager##":              "Create and alter any database",
	"##MS_DefinitionReader##":             "View the definition of any object",
	"##MS_LoginManager##":                 "Create, alter, and drop any login",
	"##MS_PerformanceDefinitionReader##":  "View any performance definition",
	"##MS_SecurityDefinitionReader##":     "View any security definition",
	"##MS_ServerPerformanceStateReader##": "View server performance state",
	"##MS_ServerSecurityStateReader##":    "View server security state",
	"##MS_ServerStateManager##":           "View and alter server state",
	"##MS_ServerStateReader##":            "View server state, including other sessions' queries",
}

var fixedServerRoleImpact = map[string]string{
	"sysadmin":      impactCritical,
	"securityadmin": impactCritical,
	"serveradmin":   impactCritical,
	"setupadmin":    impactHigh,
	"processadmin":  impactHigh,
	"diskadmin":     impactHigh,
	"dbcreator":     impactHigh,
	"bulkadmin":     impactMedium,
	"public":        impactLow,

	"##MS_LoginManager##":                 impactHigh,
	"##MS_DatabaseManager##":              impactHigh,
	"##MS_ServerStateManager##":           impactHigh,
	"##MS_DatabaseConnector##":            impactMedium,
	"##MS_DefinitionReader##":             impactMedium,
	"##MS_SecurityDefinitionReader##":     impactMedium,
	"##MS_ServerSecurityStateReader##":    impactMedium,
	"##MS_ServerStateReader##":            impactMedium,
	"##MS_PerformanceDefinitionReader##":  impactLow,
	"##MS_ServerPerformanceStateReader##": impactLow,
}

const (
	impactLow      = "Low"
	impactMedium   = "Medium"
	impactHigh     = "High"
	impactCritical = "Critical"
)

// impactRank orders the impact levels; "" (unrated) ranks below Low.
var impactRank = map[string]int{impactLow: 1, impactMedium: 2, impactHigh: 3, impactCritical: 4}

// serverPermissionImpact rates a server permission granted to a user-defined
// role, on the scale the fixed roles use: a permission rates as the fixed role
// that carries it (CONTROL SERVER is sysadmin's, ALTER SETTINGS serveradmin's,
// ADMINISTER BULK OPERATIONS bulkadmin's). One not listed rates Medium, so a
// grant this table predates is never shown as harmless.
var serverPermissionImpact = map[gosmo.ServerPermission]string{
	"CONTROL SERVER":        impactCritical,
	"IMPERSONATE ANY LOGIN": impactCritical,
	"ALTER SETTINGS":        impactCritical,
	"SHUTDOWN":              impactCritical,
	"UNSAFE ASSEMBLY":       impactCritical,

	"ALTER ANY LOGIN":              impactHigh,
	"CREATE LOGIN":                 impactHigh,
	"ALTER ANY SERVER ROLE":        impactHigh,
	"ALTER ANY CREDENTIAL":         impactHigh,
	"ALTER ANY DATABASE":           impactHigh,
	"CREATE ANY DATABASE":          impactHigh,
	"ALTER ANY CONNECTION":         impactHigh,
	"ALTER SERVER STATE":           impactHigh,
	"ALTER RESOURCES":              impactHigh,
	"ALTER ANY ENDPOINT":           impactHigh,
	"CREATE ENDPOINT":              impactHigh,
	"ALTER ANY LINKED SERVER":      impactHigh,
	"ALTER ANY AVAILABILITY GROUP": impactHigh,
	"CREATE AVAILABILITY GROUP":    impactHigh,
	"ALTER ANY SERVER AUDIT":       impactHigh,
	"ALTER TRACE":                  impactHigh,
	"ALTER ANY EVENT SESSION":      impactHigh,
	"AUTHENTICATE SERVER":          impactHigh,
	"EXTERNAL ACCESS ASSEMBLY":     impactHigh,
	"SELECT ALL USER SECURABLES":   impactHigh,

	"ADMINISTER BULK OPERATIONS":      impactMedium,
	"CONNECT ANY DATABASE":            impactMedium,
	"VIEW ANY DEFINITION":             impactMedium,
	"VIEW ANY SECURITY DEFINITION":    impactMedium,
	"VIEW SERVER STATE":               impactMedium,
	"VIEW SERVER SECURITY STATE":      impactMedium,
	"VIEW SERVER SECURITY AUDIT":      impactMedium,
	"CREATE SERVER ROLE":              impactMedium,
	"ALTER ANY EVENT NOTIFICATION":    impactMedium,
	"CREATE DDL EVENT NOTIFICATION":   impactMedium,
	"CREATE TRACE EVENT NOTIFICATION": impactMedium,

	"CONNECT SQL":                     impactLow,
	"VIEW ANY DATABASE":               impactLow,
	"VIEW ANY PERFORMANCE DEFINITION": impactLow,
	"VIEW SERVER PERFORMANCE STATE":   impactLow,
}

// serverRoleRating is one row's Impact cell and Description line.
type serverRoleRating struct {
	impact, description string
}

// serverRoleRatings rates every role in roles. A fixed role takes its entry
// from the tables above (an unknown fixed role is unrated). A user-defined
// role rates as the highest of its GRANTs in perms and of every role it is a
// member of, nested membership included — a role inside sysadmin is Critical
// with no grant of its own. A DENY adds nothing.
//
// A user-defined role with nothing found is left unrated (""), never Low:
// sys.server_permissions shows a viewer without VIEW ANY DEFINITION only the
// grants of roles it is in, and perms is nil when that read failed, so "no
// grant seen" is not "no grant". The description names what set the rating.
func serverRoleRatings(roles []*gosmo.ServerRole, perms []*gosmo.ServerPermissionEntry) map[string]serverRoleRating {
	byName := make(map[string]*gosmo.ServerRole, len(roles))
	parents := map[string][]string{} // role -> the roles it is a member of
	for _, r := range roles {
		byName[r.Name] = r
	}
	for _, r := range roles {
		for _, m := range r.Members {
			if _, isRole := byName[m]; isRole {
				parents[m] = append(parents[m], r.Name)
			}
		}
	}
	grants := map[string][]gosmo.ServerPermission{}
	for _, p := range perms {
		if p.PrincipalType == gosmo.PrincipalServerRole && p.State != gosmo.PermissionDeny {
			grants[p.Principal] = append(grants[p.Principal], p.Permission)
		}
	}

	out := make(map[string]serverRoleRating, len(roles))
	var rate func(name string, seen map[string]bool) (impact, reason string)
	rate = func(name string, seen map[string]bool) (string, string) {
		if r, ok := byName[name]; ok && isFixedServerRole(r) {
			return fixedServerRoleImpact[name], ""
		}
		if seen[name] {
			return "", ""
		}
		seen[name] = true
		var impact, reason string
		consider := func(i, why string) {
			if impactRank[i] > impactRank[impact] {
				impact, reason = i, why
			}
		}
		for _, p := range grants[name] {
			i, ok := serverPermissionImpact[p]
			if !ok {
				i = impactMedium
			}
			consider(i, "holds "+string(p))
		}
		for _, parent := range parents[name] {
			i, _ := rate(parent, seen)
			consider(i, "member of "+parent)
		}
		return impact, reason
	}
	for _, r := range roles {
		if isFixedServerRole(r) {
			out[r.Name] = serverRoleRating{impact: fixedServerRoleImpact[r.Name], description: fixedServerRoleDescriptions[r.Name]}
			continue
		}
		impact, reason := rate(r.Name, map[string]bool{})
		desc := "User-defined role"
		if reason != "" {
			desc += "; " + reason
		}
		out[r.Name] = serverRoleRating{impact: impact, description: desc}
	}
	return out
}

// isFixedServerRole reports whether r is rated from the fixed tables. public
// counts although sys.server_principals reports it is_fixed_role = 0: its
// grants are everyone's, so they say nothing about a membership.
func isFixedServerRole(r *gosmo.ServerRole) bool {
	return r.IsFixedRole || r.Name == "public"
}

// serverPermissionsForRating reads the server-scope grants serverRoleRatings
// rates user-defined roles from. Its failure is not the page's: the grants
// only fill the Impact column, so a failed read leaves user-defined roles
// unrated rather than refusing to show a membership page.
func serverPermissionsForRating(ctx context.Context, sc *db.ServerConn) []*gosmo.ServerPermissionEntry {
	perms, err := sc.Server.ServerPermissions(ctx)
	if err != nil {
		return nil
	}
	return perms
}
