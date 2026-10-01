package api

// BoundaryState is whether something refuses what a restriction forbids.
type BoundaryState string

// The two states a restriction may be in. There is no third
// (docs/decisions/0003-declared-vs-enforced-capabilities.md).
const (
	// Enforced: mcpx, or something it drives, refuses the forbidden thing,
	// and a test tries it and sees the refusal.
	Enforced BoundaryState = "enforced"
	// Advisory: nothing refuses it, and every surface that shows the
	// restriction says so in the same text.
	Advisory BoundaryState = "advisory"
)

// Boundary is one restriction mcpx declares.
type Boundary struct {
	// Name is the setting, flag or label the restriction is known by.
	Name string
	// Declares is the restriction, in one sentence.
	Declares string
	State    BoundaryState
	// Refusals name the tests that try to get round an enforced boundary.
	// Required when enforced: a claim nobody has tried to break is the thing
	// this table exists to stop. TestEveryEnforcedBoundaryNamesItsRefusal
	// checks each one exists.
	Refusals []string
	// Until names the issue that will enforce an advisory boundary.
	Until string
}

// Boundaries is every restriction mcpx declares, and whether anything
// enforces it.
//
// The place a restriction is decided to be real. A sandbox profile a runtime
// ignores, a "privileged" label nothing checks and a confirmation that a cold
// cache skips all read exactly like working restrictions; each was one, until
// somebody tried to get round it. A new restriction -- #82's plugin
// capabilities, 0002's autonomy ceiling, an OAuth scope -- is a row here,
// enforced with its refusal test or advisory with the issue that will change
// that.
func Boundaries() []Boundary {
	return []Boundary{
		{
			Name: "transport.allowedOrigins",
			Declares: "a web page on an origin that is not loopback, the daemon's own " +
				"address or listed in transport.allowedOrigins cannot use /v1 or /mcp",
			State: Enforced,
			Refusals: []string{
				"TestAWebPageCannotRunCodeThroughTheDaemon",
				"TestV1AndMCPAgreeOnOrigins",
			},
		},
		{
			Name: "script.permissions",
			Declares: "a script runs with no more than the permission profile named; " +
				"a runtime with no permission model refuses a narrowed profile",
			State: Enforced,
			Refusals: []string{
				"TestScriptPermissionsIsReachableFromEverySurface",
				"TestAStrictScriptCannotReadFilesOnARuntimeWithoutPermissions",
				"TestARestrictedProfileIsRefusedByARuntimeThatCannotEnforceIt",
			},
		},
		{
			Name: "elicit.confirmDestructive",
			Declares: "with it on, a call to a tool that may be destructive (annotated " +
				"destructiveHint, or unannotated) does not run unless somebody confirms it",
			State: Enforced,
			Refusals: []string{
				"TestADestructiveCallIsRefusedWhenNobodyConfirms",
				"TestConfirmDestructiveHoldsOnAColdDaemon",
			},
		},
		{
			Name: "autonomy.max",
			Declares: "mcpx does no more on its own than the daemon's autonomy.max allows: " +
				"a prompt, a recipe run by name or a configured hook asked for above it " +
				"is lowered to it and the answer says so, from /v1, the CLI, MCP and the " +
				"plugin alike. It does not govern code a caller sends to /v1/exec itself",
			State: Enforced,
			Refusals: []string{
				"TestV1CannotRaiseAutonomyAboveTheCeiling",
				"TestTheCeilingCannotBeRaisedAtRuntime",
				"TestTheCLICannotRaiseAutonomyAboveTheCeiling",
				"TestMCPCannotRaiseAutonomyAboveTheCeiling",
				"TestThePluginCannotRaiseAutonomyAboveTheCeiling",
				"TestHooksFromConfigDoNotRunAboveTheCeiling",
				"TestRepairCannotRaiseAutonomyAboveTheCeiling",
				"TestACallerCannotRaiseAClampedSetting",
			},
		},
		{
			Name: "privileged operations (Admin)",
			Declares: "operations marked privileged change the daemon rather than " +
				"read it; nothing checks who calls them, and every tool description " +
				"and the OpenAPI document say so",
			State: Advisory,
			Until: "#253",
		},
	}
}
