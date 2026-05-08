package controller

import (
	"context"
	"fmt"
	"sort"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	wafv1alpha1 "github.com/networking-incubator/coraza-kubernetes-operator/api/v1alpha1"
)

// -----------------------------------------------------------------------------
// Target Helpers
// -----------------------------------------------------------------------------

// hasGatewayTarget reports whether the Engine targets a Gateway resource.
func hasGatewayTarget(engine *wafv1alpha1.Engine) bool {
	if engine == nil {
		return false
	}
	return engine.Spec.Target.Type == wafv1alpha1.EngineTargetTypeGateway &&
		engine.Spec.Target.Name != ""
}

// targetLabelSelector returns the workload label selector derived from the
// Engine's target reference. For Gateway targets, the GEP-1762
// gateway.networking.k8s.io/gateway-name label is used.
//
// Returns nil if the name is empty or not a valid DNS-1035 label,
// preventing silent selector mismatches.
func targetLabelSelector(engine *wafv1alpha1.Engine) *metav1.LabelSelector {
	if engine == nil {
		return nil
	}
	switch engine.Spec.Target.Type {
	case wafv1alpha1.EngineTargetTypeGateway:
		name := engine.Spec.Target.Name
		if name == "" || len(validation.IsDNS1035Label(name)) > 0 {
			return nil
		}
		return &metav1.LabelSelector{
			MatchLabels: map[string]string{
				gatewayNameLabel: name,
			},
		}
	default:
		return nil
	}
}

// -----------------------------------------------------------------------------
// Target Validation
// -----------------------------------------------------------------------------

// gatewayGVK is the GroupVersionKind for Gateway API Gateway resources.
var gatewayGVK = schema.GroupVersionKind{
	Group:   "gateway.networking.k8s.io",
	Version: "v1",
	Kind:    "Gateway",
}

// validateTarget checks that the Engine's target Gateway exists and that no
// other Engine already targets the same Gateway. Returns true when the target
// is valid and reconciliation should continue.
func (r *EngineReconciler) validateTarget(
	ctx context.Context, log logr.Logger, req ctrl.Request, engine *wafv1alpha1.Engine,
) (bool, error) {
	if !hasGatewayTarget(engine) {
		return true, nil
	}

	gw := &unstructured.Unstructured{}
	gw.SetGroupVersionKind(gatewayGVK)
	err := r.Get(ctx, types.NamespacedName{
		Name:      engine.Spec.Target.Name,
		Namespace: engine.Namespace,
	}, gw)
	if err != nil {
		if apierrors.IsNotFound(err) || apimeta.IsNoMatchError(err) {
			return r.patchTargetNotFound(ctx, log, req, engine)
		}
		logAPIError(log, req, "Engine", err, "Failed to get Gateway", nil)
		return false, fmt.Errorf("failed to get Gateway %s: %w", engine.Spec.Target.Name, err)
	}

	conflict, err := r.hasTargetConflict(ctx, log, req, engine)
	if err != nil {
		return false, err
	}

	if conflict {
		return false, nil
	}

	setConditionTrue(&engine.Status.Conditions, engine.Generation,
		conditionTargetReady, "TargetFound",
		fmt.Sprintf("Gateway %s/%s exists", engine.Namespace, engine.Spec.Target.Name))

	return true, nil
}

// patchTargetNotFound marks the Engine as degraded because the target Gateway
// does not exist.
func (r *EngineReconciler) patchTargetNotFound(
	ctx context.Context, log logr.Logger, req ctrl.Request, engine *wafv1alpha1.Engine,
) (bool, error) {
	msg := fmt.Sprintf("Gateway %s not found in namespace %s", engine.Spec.Target.Name, engine.Namespace)
	logInfo(log, req, "Engine", "Target Gateway not found; marking Engine degraded",
		"gateway", engine.Spec.Target.Name)

	patch := client.MergeFrom(engine.DeepCopy())
	before := snapshotConditions(engine.Status.Conditions)
	setConditionFalse(&engine.Status.Conditions, engine.Generation,
		conditionTargetReady, "TargetNotFound", msg)
	applyStatusConditionDegraded(&engine.Status.Conditions, engine.Generation,
		"TargetNotFound", msg)

	if err := r.Status().Patch(ctx, engine, patch); err != nil {
		logAPIError(log, req, "Engine", err, "Failed to patch status", engine)
		return false, err
	}

	logConditionTransitions(log, req, "Engine", before, engine.Status.Conditions)
	r.Recorder.Eventf(engine, nil, "Warning", "TargetNotFound", "Reconcile",
		truncateEventNote(msg))

	return false, nil
}

// hasTargetConflict returns true if another Engine already targets the same
// Gateway and has priority (older creation timestamp, or alphabetically first
// on tie).
func (r *EngineReconciler) hasTargetConflict(
	ctx context.Context, log logr.Logger, req ctrl.Request, engine *wafv1alpha1.Engine,
) (bool, error) {
	var engineList wafv1alpha1.EngineList
	if err := r.List(ctx, &engineList, client.InNamespace(engine.Namespace)); err != nil {
		logAPIError(log, req, "Engine", err, "Failed to list Engines for conflict check", nil)
		return false, fmt.Errorf("failed to list Engines: %w", err)
	}

	var candidates []wafv1alpha1.Engine
	for _, e := range engineList.Items {
		if e.Name == engine.Name {
			continue
		}

		if !e.DeletionTimestamp.IsZero() {
			continue
		}

		if hasGatewayTarget(&e) && e.Spec.Target.Name == engine.Spec.Target.Name {
			candidates = append(candidates, e)
		}
	}

	if len(candidates) == 0 {
		return false, nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		return isOlderOrFirst(&candidates[i], &candidates[j])
	})

	winner := &candidates[0]
	if isOlderOrFirst(winner, engine) {
		msg := fmt.Sprintf("Gateway %s is already targeted by Engine %s/%s",
			engine.Spec.Target.Name, winner.Namespace, winner.Name)
		logInfo(log, req, "Engine", "Target conflict detected; marking Engine degraded",
			"conflictingEngine", winner.Name)

		patch := client.MergeFrom(engine.DeepCopy())
		before := snapshotConditions(engine.Status.Conditions)
		setConditionFalse(&engine.Status.Conditions, engine.Generation,
			conditionTargetReady, "TargetConflicting", msg)
		applyStatusConditionDegraded(&engine.Status.Conditions, engine.Generation,
			"TargetConflicting", msg)
		if err := r.Status().Patch(ctx, engine, patch); err != nil {
			logAPIError(log, req, "Engine", err, "Failed to patch status", engine)
			return false, err
		}
		logConditionTransitions(log, req, "Engine", before, engine.Status.Conditions)
		r.Recorder.Eventf(engine, nil, "Warning", "TargetConflicting", "Reconcile",
			truncateEventNote(msg))
		return true, nil
	}

	return false, nil
}

// isOlderOrFirst returns true if Engine a has priority over Engine b.
// Priority is determined by creation timestamp (earlier wins), with
// namespace/name as a tiebreaker.
func isOlderOrFirst(a, b *wafv1alpha1.Engine) bool {
	ta := a.CreationTimestamp.Time
	tb := b.CreationTimestamp.Time
	if !ta.Equal(tb) {
		return ta.Before(tb)
	}

	keyA := a.Namespace + "/" + a.Name
	keyB := b.Namespace + "/" + b.Name

	return keyA < keyB
}
