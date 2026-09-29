/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

// GatewayExposureReconciler gives exposed GameServers a public port on one shared Gateway, with a
// TCPRoute/UDPRoute per port. Registered only with --public-port-range.
type GatewayExposureReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// APIReader bypasses the cache for port allocation: a lagging cache would hand the same port to
	// two servers. Nil falls back to Client.
	APIReader client.Reader

	PortRangeMin, PortRangeMax int32
	PublicHost                 string
	GatewayName                string
	GatewayNamespace           string
	GatewayClassName           string
}

const (
	publicExposureReasonAllocated     = "Allocated"
	publicExposureReasonDisabled      = "Disabled"
	publicExposureReasonPoolExhausted = "PoolExhausted"
	publicExposureReasonNotConfigured = "NotConfigured"
)

// reader is what allocation and the Gateway rebuild list GameServers through: uncached when
// APIReader is set, see its doc.
func (r *GatewayExposureReconciler) reader() client.Reader {
	if r.APIReader != nil {
		return r.APIReader
	}
	return r.Client
}

// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=tcproutes;udproutes,verbs=get;list;watch;create;update;patch;delete

func (r *GatewayExposureReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var gs gameserversv1alpha1.GameServer
	if err := r.Get(ctx, req.NamespacedName, &gs); err != nil {
		if apierrors.IsNotFound(err) {
			// The GameServer is gone: only rebuildGateway drops its Listener.
			if err := r.rebuildGateway(ctx); err != nil {
				return ctrl.Result{}, fmt.Errorf("rebuilding shared Gateway after delete: %w", err)
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if !gs.DeletionTimestamp.IsZero() {
		// Owned TCPRoute/UDPRoute objects are garbage-collected with it; nothing else to release.
		return ctrl.Result{}, nil
	}

	var egg gameserversv1alpha1.Egg
	eggKey := types.NamespacedName{Namespace: gs.EggNamespace(), Name: gs.Spec.EggRef.Name}
	if err := r.Get(ctx, eggKey, &egg); err != nil {
		if apierrors.IsNotFound(err) {
			// GameServerReconciler already flags this as Failed; nothing for this controller to do.
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	changed, err := r.reconcileOne(ctx, &gs, &egg)
	if err != nil {
		return ctrl.Result{}, err
	}
	if changed {
		if err := r.Status().Update(ctx, &gs); err != nil {
			return ctrl.Result{}, err
		}
	}

	if err := r.rebuildGateway(ctx); err != nil {
		return ctrl.Result{}, fmt.Errorf("rebuilding shared Gateway: %w", err)
	}
	return ctrl.Result{}, nil
}

// reconcileOne brings this one GameServer's status.publicExposure and owned Routes in line with
// spec.publicExposure.enabled. It reports whether Status().Update is needed.
func (r *GatewayExposureReconciler) reconcileOne(ctx context.Context, gs *gameserversv1alpha1.GameServer, egg *gameserversv1alpha1.Egg) (bool, error) {
	if !gs.Spec.PublicExposure.Enabled {
		if err := r.pruneRoutes(ctx, gs, nil); err != nil {
			return false, err
		}
		changed := len(gs.Status.PublicExposure.Ports) > 0 || gs.Status.PublicExposure.Host != ""
		gs.Status.PublicExposure = gameserversv1alpha1.GameServerPublicExposureStatus{}
		return setExposureDisabled(gs) || changed, nil
	}

	changed := false
	ports := gameserversv1alpha1.EffectivePorts(egg, gs)
	eggPortNames := make(map[string]bool, len(ports))
	for _, ep := range ports {
		eggPortNames[ep.Name] = true
	}
	// Drop ports the Egg no longer declares (it changed since the last allocation) so the pool
	// slot they held is freed for reuse.
	kept := make([]gameserversv1alpha1.GameServerPublicExposurePort, 0, len(gs.Status.PublicExposure.Ports))
	for _, p := range gs.Status.PublicExposure.Ports {
		if eggPortNames[p.Name] {
			kept = append(kept, p)
		} else {
			changed = true
		}
	}
	gs.Status.PublicExposure.Ports = kept

	assigned := make(map[string]bool, len(kept))
	for _, p := range kept {
		assigned[p.Name] = true
	}
	var missing []gameserversv1alpha1.EggPort
	for _, ep := range ports {
		if !assigned[ep.Name] {
			missing = append(missing, ep)
		}
	}

	if len(missing) > 0 {
		used, err := usedPublicPorts(ctx, r.reader())
		if err != nil {
			return changed, err
		}
		for _, ep := range missing {
			port, ok := allocatePublicPort(r.PortRangeMin, r.PortRangeMax, used)
			if !ok {
				cond := apimeta.SetStatusCondition(&gs.Status.Conditions, metav1.Condition{
					Type: gameserversv1alpha1.ConditionPublicExposureReady, Status: metav1.ConditionFalse,
					Reason: publicExposureReasonPoolExhausted, Message: "no public port is free in the configured range",
					ObservedGeneration: gs.Generation,
				})
				return changed || cond, nil // keep whatever was already assigned before the pool ran out
			}
			used[port] = true
			gs.Status.PublicExposure.Ports = append(gs.Status.PublicExposure.Ports, gameserversv1alpha1.GameServerPublicExposurePort{Name: ep.Name, Port: port})
			changed = true
		}
		sort.Slice(gs.Status.PublicExposure.Ports, func(i, j int) bool {
			return gs.Status.PublicExposure.Ports[i].Name < gs.Status.PublicExposure.Ports[j].Name
		})
	}
	if gs.Status.PublicExposure.Host != r.PublicHost {
		gs.Status.PublicExposure.Host = r.PublicHost
		changed = true
	}

	if err := r.applyRoutes(ctx, gs, egg); err != nil {
		return changed, err
	}
	keep := make(map[string]bool, len(ports))
	for _, p := range ports {
		keep[routeName(gs, p)] = true
	}
	if err := r.pruneRoutes(ctx, gs, keep); err != nil {
		return changed, err
	}
	cond := apimeta.SetStatusCondition(&gs.Status.Conditions, metav1.Condition{
		Type: gameserversv1alpha1.ConditionPublicExposureReady, Status: metav1.ConditionTrue,
		Reason: publicExposureReasonAllocated, Message: fmt.Sprintf("%d port(s) allocated", len(gs.Status.PublicExposure.Ports)),
		ObservedGeneration: gs.Generation,
	})
	return changed || cond, nil
}

func routeName(gs *gameserversv1alpha1.GameServer, port gameserversv1alpha1.EggPort) string {
	return gs.Name + "-" + port.Name
}

// applyRoutes creates or updates one Route per port, owned by the GameServer. The backend is the
// internal Service port; the public port only exists on the Gateway Listener.
func (r *GatewayExposureReconciler) applyRoutes(ctx context.Context, gs *gameserversv1alpha1.GameServer, egg *gameserversv1alpha1.Egg) error {
	for _, p := range gameserversv1alpha1.EffectivePorts(egg, gs) {
		name := routeName(gs, p)
		parentRefs := []gatewayv1.ParentReference{{
			Name:        gatewayv1.ObjectName(r.GatewayName),
			Namespace:   ptr.To(gatewayv1.Namespace(r.GatewayNamespace)),
			SectionName: ptr.To(gatewayv1.SectionName(name)),
		}}
		backendRefs := []gatewayv1.BackendRef{{BackendObjectReference: gatewayv1.BackendObjectReference{
			Name: gatewayv1.ObjectName(gs.Name),
			Port: ptr.To(gatewayv1.PortNumber(p.ContainerPort)),
		}}}

		if p.Protocol == corev1.ProtocolUDP {
			route := &gatewayv1.UDPRoute{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: gs.Namespace}}
			_, err := controllerutil.CreateOrUpdate(ctx, r.Client, route, func() error {
				route.Spec = gatewayv1.UDPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: parentRefs},
					Rules:           []gatewayv1.UDPRouteRule{{BackendRefs: backendRefs}},
				}
				return controllerutil.SetControllerReference(gs, route, r.Scheme)
			})
			if err != nil {
				return fmt.Errorf("reconciling UDPRoute %s: %w", name, err)
			}
			continue
		}

		route := &gatewayv1.TCPRoute{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: gs.Namespace}}
		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, route, func() error {
			route.Spec = gatewayv1.TCPRouteSpec{
				CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: parentRefs},
				Rules:           []gatewayv1.TCPRouteRule{{BackendRefs: backendRefs}},
			}
			return controllerutil.SetControllerReference(gs, route, r.Scheme)
		})
		if err != nil {
			return fmt.Errorf("reconciling TCPRoute %s: %w", name, err)
		}
	}
	return nil
}

// pruneRoutes deletes the GameServer's Routes whose name is not in keep (nil = all), including
// Routes of ports the Egg no longer declares.
func (r *GatewayExposureReconciler) pruneRoutes(ctx context.Context, gs *gameserversv1alpha1.GameServer, keep map[string]bool) error {
	var tcp gatewayv1.TCPRouteList
	if err := r.List(ctx, &tcp, client.InNamespace(gs.Namespace)); err != nil {
		return err
	}
	var udp gatewayv1.UDPRouteList
	if err := r.List(ctx, &udp, client.InNamespace(gs.Namespace)); err != nil {
		return err
	}
	var stale []client.Object
	for i := range tcp.Items {
		stale = append(stale, &tcp.Items[i])
	}
	for i := range udp.Items {
		stale = append(stale, &udp.Items[i])
	}
	for _, obj := range stale {
		if keep[obj.GetName()] || !metav1.IsControlledBy(obj, gs) {
			continue
		}
		if err := r.Delete(ctx, obj); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("deleting stale route %s: %w", obj.GetName(), err)
		}
	}
	return nil
}

// rebuildGateway recomputes the shared Gateway's Listeners from every exposed GameServer. It is not
// concurrent with itself (one worker).
func (r *GatewayExposureReconciler) rebuildGateway(ctx context.Context) error {
	var list gameserversv1alpha1.GameServerList
	if err := r.reader().List(ctx, &list); err != nil {
		return err
	}

	byName := map[gatewayv1.SectionName]gatewayv1.Listener{}
	for i := range list.Items {
		gs := &list.Items[i]
		if !gs.Spec.PublicExposure.Enabled || len(gs.Status.PublicExposure.Ports) == 0 {
			continue
		}
		var egg gameserversv1alpha1.Egg
		eggKey := types.NamespacedName{Namespace: gs.EggNamespace(), Name: gs.Spec.EggRef.Name}
		if err := r.Get(ctx, eggKey, &egg); err != nil {
			continue // Egg gone or unreadable: this server's Routes just won't attach; not fatal here.
		}
		allocated := make(map[string]int32, len(gs.Status.PublicExposure.Ports))
		for _, p := range gs.Status.PublicExposure.Ports {
			allocated[p.Name] = p.Port
		}
		for _, ep := range gameserversv1alpha1.EffectivePorts(&egg, gs) {
			port, ok := allocated[ep.Name]
			if !ok {
				continue
			}
			protocol := gatewayv1.ProtocolType(ep.Protocol)
			if protocol == "" {
				protocol = gatewayv1.TCPProtocolType
			}
			name := gatewayv1.SectionName(routeName(gs, ep))
			byName[name] = gatewayv1.Listener{
				Name:     name,
				Port:     gatewayv1.PortNumber(port),
				Protocol: protocol,
				AllowedRoutes: &gatewayv1.AllowedRoutes{
					Namespaces: &gatewayv1.RouteNamespaces{From: ptr.To(gatewayv1.NamespacesFromAll)},
				},
			}
		}
	}

	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, string(n))
	}
	sort.Strings(names)
	listeners := make([]gatewayv1.Listener, 0, len(names))
	for _, n := range names {
		listeners = append(listeners, byName[gatewayv1.SectionName(n)])
	}

	if len(listeners) == 0 {
		// A Gateway needs at least one Listener: with no exposed server, delete it.
		err := r.Delete(ctx, &gatewayv1.Gateway{ObjectMeta: metav1.ObjectMeta{Name: r.GatewayName, Namespace: r.GatewayNamespace}})
		return client.IgnoreNotFound(err)
	}

	gw := &gatewayv1.Gateway{ObjectMeta: metav1.ObjectMeta{Name: r.GatewayName, Namespace: r.GatewayNamespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, gw, func() error {
		gw.Spec.GatewayClassName = gatewayv1.ObjectName(r.GatewayClassName)
		gw.Spec.Listeners = listeners
		return nil
	})
	return err
}

func (r *GatewayExposureReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gameserversv1alpha1.GameServer{}).
		Named("gatewayexposure").
		Complete(r)
}

// setExposureDisabled moves PublicExposureReady to Disabled, but only on a GameServer that ever had
// the condition: one that never opted in stays without it. Reports whether the condition changed.
func setExposureDisabled(gs *gameserversv1alpha1.GameServer) bool {
	if apimeta.FindStatusCondition(gs.Status.Conditions, gameserversv1alpha1.ConditionPublicExposureReady) == nil {
		return false
	}
	return apimeta.SetStatusCondition(&gs.Status.Conditions, metav1.Condition{
		Type: gameserversv1alpha1.ConditionPublicExposureReady, Status: metav1.ConditionFalse,
		Reason: publicExposureReasonDisabled, Message: "public exposure is disabled", ObservedGeneration: gs.Generation,
	})
}

// PublicExposureUnavailableReconciler replaces GatewayExposureReconciler without --public-port-range:
// it only marks servers that ask for exposure as NotConfigured.
type PublicExposureUnavailableReconciler struct {
	client.Client
}

func (r *PublicExposureUnavailableReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var gs gameserversv1alpha1.GameServer
	if err := r.Get(ctx, req.NamespacedName, &gs); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !gs.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}
	var changed bool
	if gs.Spec.PublicExposure.Enabled {
		changed = apimeta.SetStatusCondition(&gs.Status.Conditions, metav1.Condition{
			Type: gameserversv1alpha1.ConditionPublicExposureReady, Status: metav1.ConditionFalse,
			Reason:             publicExposureReasonNotConfigured,
			Message:            "public exposure is not configured on this platform (operator started without --public-port-range)",
			ObservedGeneration: gs.Generation,
		})
	} else {
		changed = setExposureDisabled(&gs)
	}
	if !changed {
		return ctrl.Result{}, nil
	}
	return ctrl.Result{}, r.Status().Update(ctx, &gs)
}

func (r *PublicExposureUnavailableReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gameserversv1alpha1.GameServer{}).
		Named("publicexposureunavailable").
		Complete(r)
}
