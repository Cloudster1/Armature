package test

// This file carries no build tag, so `make test` holds the integration suite
// to its rule without the stack: it only parses the other files.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// sequentialTests names each test that shares something, and what. Go runs
// them one at a time before any parallel test starts, so they overlap nothing.
var sequentialTests = map[string]string{
	// They pause replay or hold the replica, which every other test reads from,
	// or count where their own cluster sent reads.
	"TestReadsReachTheReplica":                                   "counts the reads its cluster sends to the replica",
	"TestReadYourWritesUnderLag":                                 "pauses replay on the shared replica",
	"TestLaggingReplicaLeavesTheRotation":                        "pauses replay on the shared replica",
	"TestIdleReplicaStaysHealthy":                                "needs the replica idle and caught up for three seconds",
	"TestTheInvitationsListShowsWhatWasJustSentOrWithdrawn":      "holds the shared replica",
	"TestADashboardMadeOnFirstSightIsShownAtOnce":                "holds the shared replica",
	"TestAKeyUsedAtOnceIsKnownWhileTheReplicaLags":               "holds the shared replica",
	"TestSavedPreferencesAreTheAnswerWhileTheReplicaLags":        "holds the shared replica",
	"TestAWebhookTestReadsTheDeliveryItJustWrote":                "pauses replay on the shared replica",
	"TestReplicaDownAtStartJoinsWhenItReturns":                   "watches a replica join within a deadline",
	"TestAReplyThatFailsToPostIsReadAgain":                       "puts a trigger on issue_comment, which every test writes",
	"TestOldKeysComeOverFromTheColumnThatKeptOne":                "creates databases and roles",
	"TestMigrationsRunAsPlainOwner":                              "creates databases and roles",
	"TestTheUpgradeRefusesSeveralOrganizations":                  "creates databases and roles",
	"TestRowSecurityGoesAsThePlainOwner":                         "creates databases and roles",
	"TestTheDropStopsAtAPolicyItDidNotMake":                      "creates databases and roles",
	"TestKeysAreTheInstallations":                                "creates databases and roles",
	"TestTheUpgradeRefusesKeysThatCollide":                       "creates databases and roles",
	"TestTriggersKeepTheirRulesWithoutTheOrganization":           "creates databases and roles",
	"TestTheOrganizationColumnsGoAsThePlainOwner":                "creates databases and roles",
	"TestTheTablesAreRenamedAsThePlainOwner":                     "creates databases and roles",
	"TestTheConnectedToolsAreListedToEveryoneSignedIn":           "decides which connected app stands for Stator and Exciter, which everyone shares",
	"TestOutboxRelay":                                            "relays the whole installation's outbox into the shared stream",
	"TestOutboxRelayWaitsForTheSlowestGroup":                     "relays the whole installation's outbox into the shared stream",
	"TestAConsumerRetriesAHandlerThatFailedOnce":                 "consumes the shared Redis stream",
	"TestAStoppedConsumerFinishesTheEventInHandAndLeavesTheRest": "consumes the shared Redis stream",
	"TestAKilledConsumersBatchIsHandledByTheNext":                "consumes the shared Redis stream",
	"TestAnEventThatAlwaysFailsEndsInTheDeadLetterRecord":        "consumes the shared Redis stream",
	"TestAPermanentFailureIsSetAsideAtOnce":                      "consumes the shared Redis stream",
	"TestSharedFreshnessOnlyRises":                               "uses the shared Redis and a 50ms lifetime",

	// They run a pass of the worker, which works across the whole installation.
	"TestAbsencesPastTheirWindowAreSwept":                       "runs the retention sweep over the whole installation",
	"TestPersonalDataHasALifetime":                              "runs the retention sweep over the whole installation",
	"TestTheAuditLogRecordsWhoDidWhat":                          "runs the retention sweep over the whole installation",
	"TestDeletingAnIssueRemovesItsFilesFromTheBucket":           "runs the attachment reaper over the whole installation",
	"TestDeletingAnEpicRemovesItsDescendantsFilesFromTheBucket": "runs the attachment reaper over the whole installation",
	"TestAnAttachmentRowNeverLeavesWithoutATombstone":           "runs the attachment reaper over the whole installation",
	"TestScheduledAndManualRuns":                                "runs the automation scheduler over the whole installation",
	"TestAWebhookIsSignedRetriedAndRedelivered":                 "sends every due webhook delivery in the installation",
	"TestEveryIssueThatComesOrGoesIsAnnounced":                  "sends every due webhook delivery",
	"TestAHangingWebhookDoesNotHoldUpAnotherEndpoint":           "sends every due webhook delivery in the installation, within a second",
	"TestResolvingWithoutAReplyStopsTheFirstResponseClock":      "runs the SLA watch over every desk",
	"TestResolvingAfterABreachKeepsIt":                          "runs the SLA watch over every desk",
	"TestAGoalThatCountsBusinessHoursWaitsForMonday":            "runs the SLA watch over every desk with a fake clock",
	"TestABreachIsNoticedByTheWatch":                            "runs the SLA watch over every desk",
	"TestADailyDigestGoesInOnePassAtTheDigestHour":              "runs the digest over everybody",
	"TestADigestBundlesTheQueue":                                "runs the digest over everybody",
	"TestASavedFilterIsAQueryWithAName":                         "mails every due filter subscription",
	"TestASubscriptionNobodyMayReadIsDroppedNotSent":            "mails every due filter subscription and counts the pass",
	"TestTheWorkerSnapshotsEveryProjectsSprints":                "snapshots every running sprint in the installation",
	"TestASprintIsSnapshottedDayByDay":                          "snapshots every running sprint in the installation",

	// They read the shared mailpit box.
	"TestTheMailboxIsReadOverPOP3":                          "reads the shared mailpit box",
	"TestMailsAboutOneRequestArriveEncodedInOneThread":      "reads the shared mailpit box",
	"TestMailThroughAnUntrustedCertificateNeedsInsecureTLS": "reads the shared mailpit box",
	"TestOnlyTheReporterIsAskedToRate":                      "reads the shared mailpit box",

	// They count against the brakes every caller from 127.0.0.1 shares, which
	// newAPIServer resets: run beside another test, either would trip or reset them.
	"TestInvitationLinks":                                          "trips the invitation brake",
	"TestThePublicEntryPointsRefuseBadInput":                       "counts against the invitation brake",
	"TestTheEdgesRefuseWhatABrowserWouldNotSend":                   "trips the sign-in brake",
	"TestAMailAddressIsEnoughToRaiseARequest":                      "trips the portal code brake",
	"TestACustomerPutsFilesOnTheirRequestAndTakesBackOnlyTheirOwn": "counts against the portal door brakes",
	"TestADeskMayLetItsCallersInWithoutACode":                      "counts against the portal door brakes",
	"TestAnOpenDoorCannotHandOverAnAccount":                        "counts against the portal door brakes",
	"TestAnOpenDoorCannotActForTheWholePerson":                     "counts against the portal door brakes",
	"TestAPortalCodeCannotBeGuessedByAskingAgain":                  "counts against the portal door brakes",
	"TestTheDoorIsNamedByItsDesk":                                  "counts against the portal door brakes",
	"TestADeskTrustsDomainsNotAddresses":                           "counts against the portal door brakes and removes every @elsewhere.test account",
	"TestDeskExtrasOverTheAPI":                                     "counts against the rating brake",
	"TestADashboardIsPrintedAsAPDF":                                "counts against the PDF and shared link brakes",
	"TestADashboardIsSharedByALinkUntilItIsRevoked":                "counts against the shared link brake",
	"TestAModelAnswersWithTheCallersOwnTools":                      "counts against the assistant brake",
	"TestAModelsChangeWaitsForTheReader":                           "counts against the assistant brake",
	"TestAForgottenPasswordIsResetByMail":                          "counts against the password link brakes",
	"TestAskingForAPasswordLinkTellsNothing":                       "trips the password link brakes and sets the installation's one identity provider",

	// They change what every test in the installation lives with, and put it back.
	"TestTheInstallationRequiresASecondFactor":                      "changes the second-factor rule every sign-in in the installation is held to",
	"TestASingleSignOnSessionReliesOnItsProvider":                   "changes the installation's second-factor rule and identity provider",
	"TestSigningInThroughAProvider":                                 "sets the installation's one identity provider",
	"TestAnIdentityProviderTokenIsExchangedForAGrant":               "sets the installation's one identity provider",
	"TestSomebodyWhoWasNeverInvitedIsRefused":                       "sets the installation's one identity provider",
	"TestGroupsFollowTheProvider":                                   "sets the installation's one identity provider",
	"TestAGroupTheProviderNamesCanBeCreatedOnSight":                 "sets the installation's one identity provider",
	"TestForgedTokensAreRefused":                                    "sets the installation's one identity provider",
	"TestACallbackMustBelongToASignInThatStartedHere":               "sets the installation's one identity provider",
	"TestSignInIsRefusedWhenTheProviderIsOff":                       "sets the installation's one identity provider",
	"TestTheClientSecretIsKeptOutOfSight":                           "sets the installation's one identity provider",
	"TestAProviderAccountMadeGlobalAdministratorCanAuthorWorkflows": "sets the installation's one identity provider",
	"TestAStrangerAtTheProviderCanBeLetInFromTheUsersPage":          "sets the installation's one identity provider",
	"TestAGroupWithARoleLetsSomebodyInAtSignIn":                     "sets the installation's one identity provider",
	"TestAGroupWithARoleLetsAPartnerConnectSomebodyNew":             "sets the installation's one identity provider",
	"TestHolidaysAndAbsencesReachTheCalendarAndThePlan":             "sets the default holiday calendar's days, which every plan counts with",
	"TestTheCalendarMonthCarriesTheDaysOffOfTheSixWeeksItDraws":     "sets the default holiday calendar's days, which every plan counts with",
	"TestAProjectsFallbackOutranksATenantMappingForTheType":         "changes the default workflow scheme every project inherits",
	"TestPromotingASchemeMovesEveryInheritingProject":               "makes another scheme the default every project inherits",
	"TestThemesOverTheAPI":                                          "sets everyone's default theme",
	"TestAnAccountTheProviderSignsInGetsNoPasswordHere":             "sets the installation's one identity provider",
	"TestAMoveKeepsFieldsOnEveryProjectAndNamesTheValuesItDrops":    "adds a field to every project",
	"TestFieldsOnEveryProjectAreSharedAndPromoted":                  "adds a field to every project",
	"TestInstallationLevelOverTheAPI":                               "adds a field to every project",
	"TestEveryEndpointOverTheAPI":                                   "makes a scheme the installation's default for a moment and sets up its identity provider",
	"TestHolidayCalendarsAreTheAdministratorsToKeep":                "moves the installation's default holiday calendar for a moment",
	"TestTheInstallationIsNamedByItsAdministrators":                 "renames the installation, which every session reads",
	"TestArrangementsOverTheAPI":                                    "arranges issues for every project and puts the arrangement back",
	"TestAProjectArrangesItsOwnIssues":                              "clears and sets the arrangement every project follows",
	"TestAnImportMapsWhatItFinds":                                   "makes an account the file's usernames match, which a parallel import of the same file would take up",
}

// A test that neither runs in parallel nor says why it cannot holds the whole
// suite up, and one that runs in parallel while sharing something flakes.
func TestEveryIntegrationTestRunsInParallelOrSaysWhyNot(t *testing.T) {
	t.Parallel()
	parallel, sequential, err := integrationTests(".")
	if err != nil {
		t.Fatal(err)
	}
	if len(parallel)+len(sequential) == 0 {
		t.Fatal("found no tests in this directory; the check is looking in the wrong place")
	}
	for _, name := range sequential {
		reason, listed := sequentialTests[name]
		switch {
		case !listed:
			t.Errorf("%s does not call t.Parallel() first. Call it, or add it to sequentialTests with what it shares that a parallel test would disturb.", name)
		case strings.TrimSpace(reason) == "":
			t.Errorf("%s is in sequentialTests without a reason. Say what it shares, or make it call t.Parallel().", name)
		}
	}
	found := map[string]bool{}
	for _, name := range append(parallel, sequential...) {
		found[name] = true
	}
	for _, name := range parallel {
		if _, listed := sequentialTests[name]; listed {
			t.Errorf("%s calls t.Parallel() but is in sequentialTests; take it off the list.", name)
		}
	}
	var gone []string
	for name := range sequentialTests {
		if !found[name] {
			gone = append(gone, name)
		}
	}
	sort.Strings(gone)
	for _, name := range gone {
		t.Errorf("%s is in sequentialTests but no such test exists; take it off the list.", name)
	}
}

// integrationTests sorts the top-level tests in dir by whether their first
// statement is t.Parallel().
func integrationTests(dir string) (parallel, sequential []string, err error) {
	files, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil {
		return nil, nil, err
	}
	fset := token.NewFileSet()
	for _, path := range files {
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil, nil, err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !strings.HasPrefix(fn.Name.Name, "Test") || fn.Name.Name == "TestMain" {
				continue
			}
			if callsParallelFirst(fn) {
				parallel = append(parallel, fn.Name.Name)
			} else {
				sequential = append(sequential, fn.Name.Name)
			}
		}
	}
	return parallel, sequential, nil
}

// callsParallelFirst says whether fn's first statement is t.Parallel() on its
// own *testing.T.
func callsParallelFirst(fn *ast.FuncDecl) bool {
	params := fn.Type.Params.List
	if len(params) != 1 || len(params[0].Names) != 1 || fn.Body == nil || len(fn.Body.List) == 0 {
		return false
	}
	stmt, ok := fn.Body.List[0].(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := stmt.X.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Parallel" {
		return false
	}
	recv, ok := sel.X.(*ast.Ident)
	return ok && recv.Name == params[0].Names[0].Name
}

// The check must tell the two apart, or the test above passes by seeing nothing.
func TestTheParallelCheckTellsTheTwoApart(t *testing.T) {
	t.Parallel()
	src := `package x
func TestFirst(t *testing.T) { t.Parallel(); work() }
func TestLater(t *testing.T) { work(); t.Parallel() }
func TestOther(tt *testing.T) { t.Parallel() }
func TestNone(t *testing.T) {}
`
	file, err := parser.ParseFile(token.NewFileSet(), "x_test.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, decl := range file.Decls {
		fn := decl.(*ast.FuncDecl)
		got[fn.Name.Name] = callsParallelFirst(fn)
	}
	want := map[string]bool{"TestFirst": true, "TestLater": false, "TestOther": false, "TestNone": false}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("callsParallelFirst(%s) = %v, want %v", name, got[name], w)
		}
	}
}
