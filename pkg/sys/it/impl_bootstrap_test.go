/*
 * Copyright (c) 2024-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package sys_it

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
	"github.com/voedger/voedger/pkg/appdef"
	"github.com/voedger/voedger/pkg/appparts"
	"github.com/voedger/voedger/pkg/btstrp"
	"github.com/voedger/voedger/pkg/bus"
	"github.com/voedger/voedger/pkg/checkpoints"
	"github.com/voedger/voedger/pkg/cluster"
	"github.com/voedger/voedger/pkg/coreutils"
	"github.com/voedger/voedger/pkg/coreutils/federation"
	"github.com/voedger/voedger/pkg/extensionpoints"
	"github.com/voedger/voedger/pkg/goutils/httpu"
	"github.com/voedger/voedger/pkg/goutils/logger"
	"github.com/voedger/voedger/pkg/goutils/testingu"
	"github.com/voedger/voedger/pkg/goutils/timeu"
	"github.com/voedger/voedger/pkg/iblobstoragestg"
	"github.com/voedger/voedger/pkg/istorage"
	"github.com/voedger/voedger/pkg/istructs"
	"github.com/voedger/voedger/pkg/istructsmem"
	payloads "github.com/voedger/voedger/pkg/itokens-payloads"
	"github.com/voedger/voedger/pkg/parser"
	blobprocessor "github.com/voedger/voedger/pkg/processors/blobber"
	"github.com/voedger/voedger/pkg/sys/sysprovide"
	"github.com/voedger/voedger/pkg/sys/uniques"
	it "github.com/voedger/voedger/pkg/vit"
	"github.com/voedger/voedger/pkg/vvm"
	builtinapps "github.com/voedger/voedger/pkg/vvm/builtin"
	"github.com/voedger/voedger/pkg/vvm/builtin/clusterapp"
	dbcertcache "github.com/voedger/voedger/pkg/vvm/db_cert_cache"
	vvmstorage "github.com/voedger/voedger/pkg/vvm/storage"
)

func TestBoostrap_BasicUsage(t *testing.T) {
	require := require.New(t)

	// launch the VVM with an app with a certain NumParts and NumAppWorkspaces
	numParts := istructs.NumAppPartitions(42)
	numAppWS := istructs.NumAppWorkspaces(43)
	cfg := getTestCfg(numParts, numAppWS)
	vit := it.NewVIT(t, &cfg)
	defer vit.TearDown()

	var clusterApp btstrp.ClusterBuiltInApp
	otherApps := []appparts.BuiltInApp{}
	for _, app := range vit.BuiltInAppsPackages {
		if app.Name == istructs.AppQName_sys_cluster {
			clusterApp = btstrp.ClusterBuiltInApp(app.BuiltInApp)
		} else {
			otherApps = append(otherApps, app.BuiltInApp)
		}
	}

	t.Run("basic usage", func(t *testing.T) {
		appParts, cleanup := appparts.NewTestAppParts(vit.IAppStructsProvider)
		defer cleanup()
		postWiredInterfacePtrs := newPostWiredInterfacePtrs()
		testBlobRequestHandler := blobprocessor.NewIRequestHandler(nil, 0, nil)
		testRequestSender := bus.NewIRequestSender(testingu.MockTime, nil)
		err := btstrp.Bootstrap(t.Context(), vit.IFederation.WithRetry(), vit.IAppStructsProvider, vit.Time, appParts, newMockInitialRecovery(t, vit, nil), clusterApp, otherApps,
			nil, vit.ITokens, vit.IAppStorageProvider, postWiredInterfacePtrs, testBlobRequestHandler, testRequestSender)
		require.NoError(err)
		require.NotNil(*postWiredInterfacePtrs.BlobberAppStorage)
		require.NotNil(*postWiredInterfacePtrs.RouterAppStorage)
		require.NotNil(*postWiredInterfacePtrs.RequestSender)
		require.NotNil(*postWiredInterfacePtrs.BlobHandler)

	})

	t.Run("panic on NumPartitions change", func(t *testing.T) {
		appParts, cleanup := appparts.NewTestAppParts(vit.IAppStructsProvider)
		defer cleanup()
		otherApps[0].AppDeploymentDescriptor.NumParts++
		defer func() {
			otherApps[0].AppDeploymentDescriptor.NumParts--
		}()
		postWiredInterfacePtrs := newPostWiredInterfacePtrs()
		testBlobRequestHandler := blobprocessor.NewIRequestHandler(nil, 0, nil)
		testRequestSender := bus.NewIRequestSender(testingu.MockTime, nil)
		//nolint errcheck
		require.PanicsWithValue(fmt.Sprintf("failed to deploy app %[1]s: status 409, expected [200 201]: num partitions changed: app %[1]s declaring NumPartitions=%d but was previously deployed with NumPartitions=%d",
			otherApps[0].Name, otherApps[0].AppDeploymentDescriptor.NumParts, otherApps[0].AppDeploymentDescriptor.NumParts-1), func() {
			btstrp.Bootstrap(t.Context(), vit.IFederation.WithRetry(), vit.IAppStructsProvider, vit.Time, appParts, newMockInitialRecovery(t, vit, nil), clusterApp, otherApps,
				nil, vit.ITokens, vit.IAppStorageProvider, postWiredInterfacePtrs, testBlobRequestHandler, testRequestSender)
		})
	})

	t.Run("panic on NumAppPartitions change", func(t *testing.T) {
		appParts, cleanup := appparts.NewTestAppParts(vit.IAppStructsProvider)
		defer cleanup()
		otherApps[0].AppDeploymentDescriptor.NumAppWorkspaces++
		defer func() {
			otherApps[0].AppDeploymentDescriptor.NumAppWorkspaces--
		}()

		//nolint errcheck
		require.PanicsWithValue(fmt.Sprintf("failed to deploy app %[1]s: status 409, expected [200 201]: num application workspaces changed: app %[1]s declaring NumAppWorkspaces=%d but was previously deployed with NumAppWorkspaces=%d",
			otherApps[0].Name, otherApps[0].AppDeploymentDescriptor.NumAppWorkspaces, otherApps[0].AppDeploymentDescriptor.NumAppWorkspaces-1), func() {
			postWiredInterfacePtrs := newPostWiredInterfacePtrs()
			testBlobRequestHandler := blobprocessor.NewIRequestHandler(nil, 0, nil)
			testRequestSender := bus.NewIRequestSender(testingu.MockTime, nil)
			btstrp.Bootstrap(t.Context(), vit.IFederation.WithRetry(), vit.IAppStructsProvider, vit.Time, appParts, newMockInitialRecovery(t, vit, nil), clusterApp, otherApps,
				nil, vit.ITokens, vit.IAppStorageProvider, postWiredInterfacePtrs, testBlobRequestHandler, testRequestSender)
		})
	})
}

func TestBootstrapInitialRecoveryOrchestration(t *testing.T) {
	cfg := getTestCfg(3, 1)
	vit := it.NewVIT(t, &cfg)
	defer vit.TearDown()
	clusterApp, otherApps := bootstrapTestApps(vit)
	require.NotEmpty(t, otherApps)
	var sidecarApps []appparts.SidecarApp
	for index, app := range otherApps {
		if app.Name == istructs.AppQName_test1_app1 {
			sidecarApps = []appparts.SidecarApp{{BuiltInApp: app}}
			otherApps = append(otherApps[:index:index], otherApps[index+1:]...)
			break
		}
	}
	require.Len(t, sidecarApps, 1)
	targetApp := sidecarApps[0].Name

	t.Run("initializes every registered app before its partitions", func(t *testing.T) {
		appParts, cleanup := appparts.NewTestAppParts(vit.IAppStructsProvider)
		defer cleanup()
		timeline := &bootstrapOperationsOfApps{}
		recordingParts := &bootstrapRecordingAppParts{IAppPartitions: appParts, timeline: timeline}
		initializer := newMockInitialRecovery(t, vit, timeline)

		err := runBootstrap(t.Context(), vit, recordingParts, initializer, clusterApp, otherApps, sidecarApps)

		require.NoError(t, err)
		apps := []appparts.BuiltInApp{appparts.BuiltInApp(clusterApp)}
		apps = append(apps, otherApps...)
		apps = append(apps, sidecarApps[0].BuiltInApp)

		// check operations order: registrered, initialized, partitions
		for _, app := range apps {
			order_DeployApp := timeline.requireOrder(operationDeployApp, app.Name)
			order_InitializeCheckpoints := timeline.requireOrder(operationInitializeCheckpoints, app.Name)
			order_DeployAppPartitions := timeline.requireOrder(operationDeployAppPartitions, app.Name)
			require.Less(t, order_DeployApp, order_InitializeCheckpoints, app.Name.String())
			require.Less(t, order_InitializeCheckpoints, order_DeployAppPartitions, app.Name.String())
			require.Equal(t, app.NumParts, initializer.numPartitions(app.Name))
			require.ElementsMatch(t, initializer.deployable[app.Name], timeline.partitionIDs(app.Name))
		}
	})

	for _, test := range []struct {
		name       string
		failed     []istructs.PartitionID
		deployable []istructs.PartitionID
	}{
		{"one partition", []istructs.PartitionID{1}, []istructs.PartitionID{0, 2}},
		{"all partitions", []istructs.PartitionID{0, 1, 2}, nil},
	} {
		t.Run(test.name+" failures exclude partitions without stopping readiness", func(t *testing.T) {
			appParts, cleanup := appparts.NewTestAppParts(vit.IAppStructsProvider)
			defer cleanup()
			timeline := &bootstrapOperationsOfApps{}
			recordingParts := &bootstrapRecordingAppParts{IAppPartitions: appParts, timeline: timeline}
			initializer := newMockInitialRecovery(t, vit, timeline)
			initializer.partitionFailures = map[appdef.AppQName]map[istructs.PartitionID]error{
				targetApp: {},
			}
			for _, id := range test.failed {
				initializer.partitionFailures[targetApp][id] = errors.New("checkpoint read failed")
			}
			logCap := logger.StartCapture(t, logger.LogLevelVerbose)
			recordingParts.beforeDeploy = func(app appdef.AppQName) {
				if app != targetApp {
					return
				}
				for _, id := range test.failed {
					logCap.HasLine("stage=bootstrap.initial_recovery.error", "app="+targetApp.String(),
						fmt.Sprintf("partid=%d", id), "checkpoint read failed")
				}
			}

			err := runBootstrap(t.Context(), vit, recordingParts, initializer, clusterApp, otherApps, sidecarApps)

			require.NoError(t, err, "successful bootstrap is the readiness signal")
			require.ElementsMatch(t, test.deployable, timeline.partitionIDs(targetApp))
			require.Equal(t, len(test.deployable) == 0, timeline.notMade(operationDeployAppPartitions, targetApp))
			for _, id := range test.failed {
				logCap.HasLine("stage=bootstrap.initial_recovery.error", "level=ERROR", "app="+targetApp.String(),
					fmt.Sprintf("partid=%d", id), "get partition checkpoint", "checkpoint read failed")
				require.Equal(t, 1, countBootstrapLogLines(logCap.String(), "stage=bootstrap.initial_recovery.error",
					"app="+targetApp.String(), fmt.Sprintf("partid=%d", id)), "bootstrap and initializer must not log twice")
				partition, err := appParts.Borrow(targetApp, id, appparts.ProcessorKind_Command)
				require.Nil(t, partition)
				require.ErrorIs(t, err, appparts.ErrNotFound)
			}
		})
	}

	t.Run("unavailable cluster validates existing compatible apps without commands", func(t *testing.T) {
		appParts, cleanup := appparts.NewTestAppParts(vit.IAppStructsProvider)
		defer cleanup()
		timeline := &bootstrapOperationsOfApps{}
		recordingParts := &bootstrapRecordingAppParts{IAppPartitions: appParts, timeline: timeline}
		initializer := newMockInitialRecovery(t, vit, timeline)
		initializer.partitionFailures = map[appdef.AppQName]map[istructs.PartitionID]error{
			istructs.AppQName_sys_cluster: {0: errors.New("cluster checkpoint unavailable")},
		}
		noCommands := &bootstrapNoCommandFederation{}
		err := runBootstrap(t.Context(), vit, recordingParts, initializer, clusterApp, otherApps, sidecarApps, noCommands)
		require.NoError(t, err)
		require.True(t, timeline.notMade(operationDeployAppPartitions, istructs.AppQName_sys_cluster))
		require.ElementsMatch(t, []istructs.PartitionID{0, 1, 2}, timeline.partitionIDs(targetApp))
		require.Zero(t, noCommands.calls)
	})

	t.Run("unavailable cluster still rejects incompatible descriptors", func(t *testing.T) {
		appParts, cleanup := appparts.NewTestAppParts(vit.IAppStructsProvider)
		defer cleanup()
		initializer := newMockInitialRecovery(t, vit, nil)
		initializer.partitionFailures = map[appdef.AppQName]map[istructs.PartitionID]error{
			istructs.AppQName_sys_cluster: {0: errors.New("cluster checkpoint unavailable")},
		}
		incompatible := append([]appparts.SidecarApp(nil), sidecarApps...)
		incompatible[0].NumParts++
		noCommands := &bootstrapNoCommandFederation{}
		err := runBootstrap(t.Context(), vit, appParts, initializer, clusterApp, otherApps, incompatible, noCommands)
		require.ErrorContains(t, err, "num partitions changed")
		require.Zero(t, noCommands.calls)
	})

	t.Run("application setup error stops partition deployment", func(t *testing.T) {
		appParts, cleanup := appparts.NewTestAppParts(vit.IAppStructsProvider)
		defer cleanup()
		timeline := &bootstrapOperationsOfApps{}
		recordingParts := &bootstrapRecordingAppParts{IAppPartitions: appParts, timeline: timeline}
		setupErr := errors.New("application structures unavailable")
		initializer := newMockInitialRecovery(t, vit, timeline)
		initializer.setupErrors = map[appdef.AppQName]error{
			otherApps[0].Name: setupErr,
		}

		err := runBootstrap(t.Context(), vit, recordingParts, initializer, clusterApp, otherApps, sidecarApps)

		require.ErrorIs(t, err, setupErr)
		require.True(t, timeline.notMade(operationDeployAppPartitions, otherApps[0].Name))
	})

	t.Run("bootstrap cancellation stops partition deployment", func(t *testing.T) {
		appParts, cleanup := appparts.NewTestAppParts(vit.IAppStructsProvider)
		defer cleanup()
		timeline := &bootstrapOperationsOfApps{}
		recordingParts := &bootstrapRecordingAppParts{IAppPartitions: appParts, timeline: timeline}
		initializer := newMockInitialRecovery(t, vit, timeline)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		err := runBootstrap(ctx, vit, recordingParts, initializer, clusterApp, otherApps, sidecarApps)

		require.ErrorIs(t, err, context.Canceled)
		require.True(t, timeline.notMade(operationDeployAppPartitions, istructs.AppQName_sys_cluster))
	})

	t.Run("cancellation after a completed partition prevents partial deployment", func(t *testing.T) {
		appParts, cleanup := appparts.NewTestAppParts(vit.IAppStructsProvider)
		defer cleanup()
		timeline := &bootstrapOperationsOfApps{}
		parts := &bootstrapRecordingAppParts{IAppPartitions: appParts, timeline: timeline}
		initializer := newMockInitialRecovery(t, vit, timeline)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		initializer.checkpointReadHook = func(app appdef.AppQName, id istructs.PartitionID) {
			if app == targetApp && id == 0 {
				cancel()
			}
		}
		err := runBootstrap(ctx, vit, parts, initializer, clusterApp, otherApps, sidecarApps)
		require.ErrorIs(t, err, context.Canceled)
		require.True(t, timeline.notMade(operationDeployAppPartitions, targetApp))
	})
}

type bootstrapNoCommandFederation struct {
	federation.IFederationWithRetry
	calls int
}

func TestBootstrapDefersFirstDeploymentWithoutClusterPartition(t *testing.T) {
	// Fail only the cluster completion-marker read. Workspace setup remains usable,
	// so the failure must exclude the partition without aborting startup.
	keyCapture := &bootstrapCheckpointKeyCapture{}
	_, _, err := vvmstorage.NewRecoveryCheckpointStorage(keyCapture).
		GetPartitionCheckpoint(istructs.ClusterApps[istructs.AppQName_sys_cluster], 0)
	require.NoError(t, err)
	logCap := logger.StartCapture(t, logger.LogLevelVerbose)
	cfg := getTestCfg(3, 1, it.WithVVMConfig(func(cfg *vvm.VVMConfig) {
		underlyingFactory := cfg.StorageFactory
		cfg.StorageFactory = func(tm timeu.ITime) (istorage.IAppStorageFactory, error) {
			factory, err := underlyingFactory(tm)
			if err != nil {
				return nil, err
			}
			return &bootstrapCheckpointFailureFactory{IAppStorageFactory: factory, key: keyCapture.key}, nil
		}
	}))
	vit := it.NewVIT(t, &cfg)
	defer vit.TearDown()
	require.NotNil(t, vit.VoedgerVM, "partition-scoped cluster failure must not block readiness")
	app := istructs.AppQName_test1_app1
	count, err := vit.IAppPartitions.AppPartsCount(app)
	require.NoError(t, err)
	require.Equal(t, istructs.NumAppPartitions(3), count, "deferred app must remain registered for routing")
	clusterStructs, err := vit.BuiltIn(istructs.AppQName_sys_cluster)
	require.NoError(t, err)
	id, err := uniques.GetRecordIDByUniqueCombination(clusterapp.ClusterAppWSID,
		appdef.NewQName(cluster.ClusterPackage, "App"), clusterStructs, map[string]interface{}{cluster.Field_AppQName: app.String()})
	require.NoError(t, err)
	require.Equal(t, istructs.NullRecordID, id, "deferral must not create a deployment descriptor")
	appStructs, err := vit.BuiltIn(app)
	require.NoError(t, err)
	appWSID := istructs.NewWSID(istructs.CurrentClusterID(), istructs.FirstBaseAppWSID)
	workspace, err := appStructs.Records().GetSingleton(appWSID, appdef.QNameCDocWorkspaceDescriptor)
	require.NoError(t, err)
	require.Equal(t, appdef.NullQName, workspace.QName(), "deferred app workspaces must not be provisioned")
	for partitionID := istructs.PartitionID(0); partitionID < 3; partitionID++ {
		partition, err := vit.IAppPartitions.Borrow(app, partitionID, appparts.ProcessorKind_Command)
		require.Nil(t, partition)
		require.ErrorIs(t, err, appparts.ErrNotFound)
	}
	response := vit.PostApp(app, appWSID, "c.sys.CUD", "{}", httpu.WithNoRetryPolicy(), httpu.Expect500())
	var responseError coreutils.SysError
	require.ErrorAs(t, response.SysError, &responseError)
	require.Equal(t, http.StatusInternalServerError, responseError.HTTPStatus)
	require.Equal(t, "partition not found", responseError.Message)
	logCap.HasLine("stage=bootstrap.initial_recovery.error", "app=sys/cluster", "partid=0", "cluster checkpoint unavailable")
	require.Positive(t, countBootstrapLogLines(strings.ToLower(logCap.String()), "defer", app.String()))
	logCap.NotContains("stage=cp.partition_recovery.start", "stage=cp.workspace_recovery.start")
}

type bootstrapCheckpointKeyCapture struct {
	vvmstorage.ISysVvmStorage
	key []byte
}

func (s *bootstrapCheckpointKeyCapture) Get(key, _ []byte, _ *[]byte) (bool, error) {
	s.key = bytes.Clone(key)
	return false, nil
}

type bootstrapCheckpointFailureFactory struct {
	istorage.IAppStorageFactory
	key []byte
}

func (f *bootstrapCheckpointFailureFactory) AppStorage(name istorage.SafeAppName) (istorage.IAppStorage, error) {
	storage, err := f.IAppStorageFactory.AppStorage(name)
	if err != nil || !strings.HasPrefix(name.String(), "sysvvm") {
		return storage, err
	}
	return &bootstrapCheckpointFailureStorage{IAppStorage: storage, key: f.key}, nil
}

type bootstrapCheckpointFailureStorage struct {
	istorage.IAppStorage
	key []byte
}

func (s *bootstrapCheckpointFailureStorage) Get(key, columns []byte, value *[]byte) (bool, error) {
	if bytes.Equal(key, s.key) {
		return false, errors.New("cluster checkpoint unavailable")
	}
	return s.IAppStorage.Get(key, columns, value)
}

func (f *bootstrapNoCommandFederation) AdminFunc(string, string, ...httpu.ReqOptFunc) (*federation.FuncResponse, error) {
	f.calls++
	return nil, errors.New("unexpected command to excluded cluster partition")
}

func countBootstrapLogLines(logs string, fragments ...string) int {
	count := 0
	for _, line := range strings.Split(logs, "\n") {
		matches := true
		for _, fragment := range fragments {
			matches = matches && strings.Contains(line, fragment)
		}
		if matches {
			count++
		}
	}
	return count
}

func bootstrapTestApps(vit *it.VIT) (btstrp.ClusterBuiltInApp, []appparts.BuiltInApp) {
	var clusterApp btstrp.ClusterBuiltInApp
	otherApps := []appparts.BuiltInApp{}
	for _, app := range vit.BuiltInAppsPackages {
		if app.Name == istructs.AppQName_sys_cluster {
			clusterApp = btstrp.ClusterBuiltInApp(app.BuiltInApp)
		} else {
			otherApps = append(otherApps, app.BuiltInApp)
		}
	}
	return clusterApp, otherApps
}

func runBootstrap(ctx context.Context, vit *it.VIT, appParts appparts.IAppPartitions,
	initializer checkpoints.IInitialRecovery, clusterApp btstrp.ClusterBuiltInApp,
	otherApps []appparts.BuiltInApp, sidecarApps []appparts.SidecarApp,
	federations ...federation.IFederationWithRetry) error {
	postWiredInterfacePtrs := newPostWiredInterfacePtrs()
	testBlobRequestHandler := blobprocessor.NewIRequestHandler(nil, 0, nil)
	testRequestSender := bus.NewIRequestSender(testingu.MockTime, nil)
	federationWithRetry := vit.IFederation.WithRetry()
	if len(federations) > 0 {
		federationWithRetry = federations[0]
	}
	return btstrp.Bootstrap(ctx, federationWithRetry, vit.IAppStructsProvider, vit.Time, appParts, initializer,
		clusterApp, otherApps, sidecarApps, vit.ITokens, vit.IAppStorageProvider, postWiredInterfacePtrs,
		testBlobRequestHandler, testRequestSender)
}

type bootstrapOperation int

const (
	operationDeployApp bootstrapOperation = iota
	operationInitializeCheckpoints
	operationDeployAppPartitions
)

type bootsrtapOperationOfApp struct {
	operation    bootstrapOperation
	appQName     appdef.AppQName
	partitionIDs []istructs.PartitionID
}

// need to track the correct order of methods call:
// DeployApp() -> InitializeCheckpoints() -> DeployAppPartitions()
type bootstrapOperationsOfApps struct {
	mu         sync.Mutex
	operations []bootsrtapOperationOfApp
}

func (l *bootstrapOperationsOfApps) append(operation bootstrapOperation, appQName appdef.AppQName, ids ...istructs.PartitionID) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.operations = append(l.operations, bootsrtapOperationOfApp{operation: operation, appQName: appQName,
		partitionIDs: append([]istructs.PartitionID(nil), ids...)})
}

func (l *bootstrapOperationsOfApps) partitionIDs(app appdef.AppQName) []istructs.PartitionID {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, operation := range l.operations {
		if operation.operation == operationDeployAppPartitions && operation.appQName == app {
			return append([]istructs.PartitionID(nil), operation.partitionIDs...)
		}
	}
	return nil
}

func (l *bootstrapOperationsOfApps) requireOrder(operation bootstrapOperation, appQName appdef.AppQName) int {
	index := l.index(operation, appQName)
	if index < 0 {
		panic(fmt.Sprintf("bootstrap operation %d was not made for app %s", operation, appQName))
	}
	return index
}

func (l *bootstrapOperationsOfApps) notMade(operation bootstrapOperation, appQName appdef.AppQName) bool {
	return l.index(operation, appQName) < 0
}

func (l *bootstrapOperationsOfApps) index(operation bootstrapOperation, appQName appdef.AppQName) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	for index, actual := range l.operations {
		if actual.operation == operation && actual.appQName == appQName {
			return index
		}
	}
	return -1
}

type bootstrapRecordingAppParts struct {
	appparts.IAppPartitions
	timeline     *bootstrapOperationsOfApps
	beforeDeploy func(appdef.AppQName)
}

func (p *bootstrapRecordingAppParts) DeployApp(name appdef.AppQName, extModuleURLs map[string]*url.URL,
	def appdef.IAppDef, partsCount istructs.NumAppPartitions, numEngines [appparts.ProcessorKind_Count]uint,
	numAppWorkspaces istructs.NumAppWorkspaces) {
	p.timeline.append(operationDeployApp, name)
	p.IAppPartitions.DeployApp(name, extModuleURLs, def, partsCount, numEngines, numAppWorkspaces)
}

func (p *bootstrapRecordingAppParts) DeployAppPartitions(name appdef.AppQName, partitionIDs []istructs.PartitionID) {
	if p.beforeDeploy != nil {
		p.beforeDeploy(name)
	}
	p.timeline.append(operationDeployAppPartitions, name, partitionIDs...)
	p.IAppPartitions.DeployAppPartitions(name, partitionIDs)
}

type mockInitialRecovery struct {
	checkpoints.IInitialRecovery
	mu                 sync.Mutex
	timeline           *bootstrapOperationsOfApps
	parts              map[appdef.AppQName]istructs.NumAppPartitions
	setupErrors        map[appdef.AppQName]error
	partitionFailures  map[appdef.AppQName]map[istructs.PartitionID]error
	appQNamesByID      map[istructs.ClusterAppID]appdef.AppQName
	deployable         map[appdef.AppQName][]istructs.PartitionID
	checkpointReadHook func(appdef.AppQName, istructs.PartitionID)
}

var _ checkpoints.IInitialRecovery = (*mockInitialRecovery)(nil)

func newMockInitialRecovery(t *testing.T, vit *it.VIT, timeline *bootstrapOperationsOfApps) *mockInitialRecovery {
	t.Helper()
	sysVVMStorage, err := vit.IAppStorageProvider.AppStorage(istructs.AppQName_sys_vvm)
	require.NoError(t, err)
	recovery := &mockInitialRecovery{
		timeline:      timeline,
		parts:         map[appdef.AppQName]istructs.NumAppPartitions{},
		appQNamesByID: map[istructs.ClusterAppID]appdef.AppQName{},
		deployable:    map[appdef.AppQName][]istructs.PartitionID{},
	}
	appStructsProvider := &mockInitialRecoveryAppStructsProvider{
		IAppStructsProvider: vit.IAppStructsProvider,
		recovery:            recovery,
	}
	checkpointStorage := &mockInitialRecoveryCheckpointStorage{
		IRecoveryCheckpointStorage: vvmstorage.NewRecoveryCheckpointStorage(sysVVMStorage),
		recovery:                   recovery,
	}
	recovery.IInitialRecovery = checkpoints.NewInitialRecovery(appStructsProvider, checkpointStorage)
	return recovery
}

func (r *mockInitialRecovery) InitializeCheckpoints(ctx context.Context, appQName appdef.AppQName,
	numPartitions istructs.NumAppPartitions) ([]istructs.PartitionID, map[istructs.PartitionID]error, error) {
	r.mu.Lock()
	r.parts[appQName] = numPartitions
	r.mu.Unlock()

	deployable, failed, err := r.IInitialRecovery.InitializeCheckpoints(ctx, appQName, numPartitions)
	r.mu.Lock()
	r.deployable[appQName] = append([]istructs.PartitionID(nil), deployable...)
	r.mu.Unlock()
	if err == nil && r.timeline != nil {
		r.timeline.append(operationInitializeCheckpoints, appQName)
	}
	return deployable, failed, err
}

func (r *mockInitialRecovery) numPartitions(appQName appdef.AppQName) istructs.NumAppPartitions {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.parts[appQName]
}

type mockInitialRecoveryAppStructsProvider struct {
	istructs.IAppStructsProvider
	recovery *mockInitialRecovery
}

func (p *mockInitialRecoveryAppStructsProvider) BuiltIn(appQName appdef.AppQName) (istructs.IAppStructs, error) {
	p.recovery.mu.Lock()
	setupErr := p.recovery.setupErrors[appQName]
	p.recovery.mu.Unlock()
	if setupErr != nil {
		return nil, setupErr
	}
	appStructs, err := p.IAppStructsProvider.BuiltIn(appQName)
	if err == nil {
		p.recovery.mu.Lock()
		p.recovery.appQNamesByID[appStructs.ClusterAppID()] = appQName
		p.recovery.mu.Unlock()
	}
	return appStructs, err
}

type mockInitialRecoveryCheckpointStorage struct {
	checkpoints.IRecoveryCheckpointStorage
	recovery *mockInitialRecovery
}

func (s *mockInitialRecoveryCheckpointStorage) GetPartitionCheckpoint(appID istructs.ClusterAppID,
	partitionID istructs.PartitionID) (checkpoints.PartitionCheckpoint, bool, error) {
	s.recovery.mu.Lock()
	appQName := s.recovery.appQNamesByID[appID]
	partitionFailure := s.recovery.partitionFailures[appQName][partitionID]
	s.recovery.mu.Unlock()
	if partitionFailure != nil {
		return checkpoints.PartitionCheckpoint{}, false, partitionFailure
	}
	checkpoint, ok, err := s.IRecoveryCheckpointStorage.GetPartitionCheckpoint(appID, partitionID)
	if s.recovery.checkpointReadHook != nil {
		s.recovery.checkpointReadHook(appQName, partitionID)
	}
	return checkpoint, ok, err
}

func newPostWiredInterfacePtrs() btstrp.PostWireInterfacePtrs {
	return btstrp.PostWireInterfacePtrs{
		BlobberAppStorage: iblobstoragestg.BlobAppStoragePtr(new(istorage.IAppStorage)),
		RouterAppStorage:  dbcertcache.RouterAppStoragePtr(new(istorage.IAppStorage)),
		BlobHandler:       blobprocessor.IRequestHandlerPtr(new(blobprocessor.IRequestHandler)),
		RequestSender:     bus.IRequestSenderPtr(new(bus.IRequestSender)),
	}
}

func getTestCfg(numParts istructs.NumAppPartitions, numAppWS istructs.NumAppWorkspaces, opts ...it.VITConfigOptFunc) it.VITConfig {
	fs := fstest.MapFS{
		"app.vsql": &fstest.MapFile{
			Data: []byte(`APPLICATION app1();`),
		},
	}
	app1PackageFS := parser.PackageFS{
		Path: it.App1PkgPath,
		FS:   fs,
	}
	return it.NewOwnVITConfig(append([]it.VITConfigOptFunc{
		it.WithApp(istructs.AppQName_test1_app1, func(apis builtinapps.APIs, cfg *istructsmem.AppConfigType, ep extensionpoints.IExtensionPoint) builtinapps.Def {
			sysPkg := sysprovide.Provide(cfg)
			return builtinapps.Def{
				NumParts:         numParts,
				EnginePoolSize:   it.DefaultTestAppEnginesPool,
				NumAppWorkspaces: numAppWS,
				AppQName:         istructs.AppQName_test1_app1,
				Packages:         []parser.PackageFS{sysPkg, app1PackageFS},
			}
		}),
	}, opts...)...)
}

func TestDeployAppErrors(t *testing.T) {
	require := require.New(t)
	vit := it.NewVIT(t, &it.SharedConfig_App1)
	defer vit.TearDown()

	sysToken, err := payloads.GetSystemPrincipalToken(vit.ITokens, istructs.AppQName_sys_cluster)
	require.NoError(err)

	t.Run("sys/cluster can not be deployed by c.cluster.DeployApp", func(t *testing.T) {
		body := fmt.Sprintf(`{"args":{"AppQName":%q,"NumPartitions":1,"NumAppWorkspaces":1}}`, istructs.AppQName_sys_cluster)
		vit.PostApp(istructs.AppQName_sys_cluster, clusterapp.ClusterAppPseudoWSID, "c.cluster.DeployApp", body,
			httpu.WithAuthorizeBy(sysToken), httpu.Expect400()).Println()
	})

	var test1App1DeploymentDescriptor appparts.AppDeploymentDescriptor
	for _, app := range vit.BuiltInAppsPackages {
		if app.Name == istructs.AppQName_test1_app1 {
			test1App1DeploymentDescriptor = app.AppDeploymentDescriptor
			break
		}
	}

	t.Run("409 conflict on try to deploy with different NumPartitions", func(t *testing.T) {
		body := fmt.Sprintf(`{"args":{"AppQName":%q,"NumPartitions":%d,"NumAppWorkspaces":%d}}`,
			istructs.AppQName_test1_app1,
			test1App1DeploymentDescriptor.NumParts+1, test1App1DeploymentDescriptor.NumAppWorkspaces)
		resp := vit.PostApp(istructs.AppQName_sys_cluster, clusterapp.ClusterAppPseudoWSID, "c.cluster.DeployApp", body,
			httpu.WithAuthorizeBy(sysToken),
			httpu.Expect409(),
		)
		resp.Println()
		require.Empty(resp.NewIDs)
	})

	t.Run("409 conflict on try to deploy with different NumAppPartitions", func(t *testing.T) {
		body := fmt.Sprintf(`{"args":{"AppQName":%q,"NumPartitions":%d,"NumAppWorkspaces":%d}}`,
			istructs.AppQName_test1_app1,
			test1App1DeploymentDescriptor.NumParts, test1App1DeploymentDescriptor.NumAppWorkspaces+1)
		resp := vit.PostApp(istructs.AppQName_sys_cluster, clusterapp.ClusterAppPseudoWSID, "c.cluster.DeployApp", body,
			httpu.WithAuthorizeBy(sysToken),
			httpu.Expect409(),
		)
		resp.Println()
		require.Empty(resp.NewIDs)
	})

	t.Run("400 bad request on wrong appQName", func(t *testing.T) {
		body := `{"args":{"AppQName":"wrong","NumPartitions":1,"NumAppWorkspaces":1}}`
		vit.PostApp(istructs.AppQName_sys_cluster, clusterapp.ClusterAppPseudoWSID, "c.cluster.DeployApp", body,
			httpu.WithAuthorizeBy(sysToken),
			httpu.Expect400(),
		).Println()
	})
}

func TestAppWSInitIndempotency(t *testing.T) {
	require := require.New(t)
	vit := it.NewVIT(t, &it.SharedConfig_App1)
	defer vit.TearDown()

	checkCDocsWSDesc(vit.VVMConfig, vit.VVM, require)

	// init app ws again (first is done on NewVIT()) -> expect no errors + assume next tests will work as well
	for _, app := range vit.BuiltInAppsPackages {
		as, err := vit.BuiltIn(app.Name)
		require.NoError(err)
		initedWSIDs, err := cluster.InitAppWSes(as, as.NumAppWorkspaces(), app.NumParts, istructs.UnixMilli(vit.Time.Now().UnixMilli()))
		require.NoError(err)
		require.Empty(initedWSIDs)
	}
}

func checkCDocsWSDesc(vvmCfg *vvm.VVMConfig, vvm *vvm.VVM, require *require.Assertions) {
	for appQName := range vvmCfg.VVMAppsBuilder {
		as, err := vvm.BuiltIn(appQName)
		require.NoError(err)
		for wsNum := 0; istructs.NumAppWorkspaces(wsNum) < as.NumAppWorkspaces(); wsNum++ {
			appWSID := istructs.NewWSID(istructs.CurrentClusterID(), istructs.WSID(wsNum+int(istructs.FirstBaseAppWSID)))
			existingCDocWSDesc, err := as.Records().GetSingleton(appWSID, appdef.QNameCDocWorkspaceDescriptor)
			require.NoError(err)
			require.Equal(appdef.QNameCDocWorkspaceDescriptor, existingCDocWSDesc.QName())
		}
	}
}

func TestVVMLaunch_VSQLCodeMismatch(t *testing.T) {
	launchAndRecover := func(t *testing.T, vsql string, registerExtraCmd bool) (panicErr error) {
		t.Helper()
		cfg := getMismatchVITCfg(vsql, registerExtraCmd)
		defer func() {
			if r := recover(); r != nil {
				panicErr, _ = r.(error)
			}
		}()
		vit := it.NewVIT(t, &cfg)
		vit.TearDown()
		return nil
	}

	t.Run("in vsql, not in code", func(t *testing.T) {
		require := require.New(t)
		vsql := `APPLICATION app1();
			ALTERABLE WORKSPACE test_wsWS (
				DESCRIPTOR test_ws (IntFld int32 NOT NULL);
				EXTENSION ENGINE BUILTIN (COMMAND missingCmd());
			);`
		err := launchAndRecover(t, vsql, false)
		require.ErrorIs(err, appparts.ErrDeployment, err)
		require.ErrorContains(err, "in vsql, not in code")
		require.ErrorContains(err, "missingCmd")
		log.Println(err)
	})

	t.Run("in code, not in vsql", func(t *testing.T) {
		require := require.New(t)
		vsql := `APPLICATION app1();
			ALTERABLE WORKSPACE test_wsWS (
				DESCRIPTOR test_ws (IntFld int32 NOT NULL);
			);`
		err := launchAndRecover(t, vsql, true)
		require.ErrorIs(err, appparts.ErrDeployment, err)
		require.ErrorContains(err, "in code, not in vsql")
		require.ErrorContains(err, "extraCmd")
		log.Println(err)
	})
}

func getMismatchVITCfg(vsql string, registerExtraCmd bool) it.VITConfig {
	const extraCmdName = "extraCmd"
	app1PackageFS := parser.PackageFS{
		Path: it.App1PkgPath,
		FS:   fstest.MapFS{"app.vsql": &fstest.MapFile{Data: []byte(vsql)}},
	}
	return it.NewOwnVITConfig(
		it.WithApp(istructs.AppQName_test1_app1, func(_ builtinapps.APIs, cfg *istructsmem.AppConfigType, _ extensionpoints.IExtensionPoint) builtinapps.Def {
			sysPkg := sysprovide.Provide(cfg)
			if registerExtraCmd {
				cfg.Resources.Add(istructsmem.NewCommandFunction(
					appdef.NewQName("app1pkg", extraCmdName), istructsmem.NullCommandExec))
			}
			return builtinapps.Def{
				AppDeploymentDescriptor: it.TestAppDeploymentDescriptor,
				AppQName:                istructs.AppQName_test1_app1,
				Packages:                []parser.PackageFS{sysPkg, app1PackageFS},
			}
		}),
	)
}
