package prune

import (
	"fmt"

	backupv1alpha1 "git.vshn.net/vshn/baas/apis/backup/v1alpha1"
	"git.vshn.net/vshn/baas/service"
	"git.vshn.net/vshn/baas/service/observe"
	"k8s.io/apimachinery/pkg/runtime"
)

type Pruner struct {
	service.CommonObjects
	observer *observe.Observer
	config   config
}

// NewPruner returns a new pruner handler
func NewPruner(common service.CommonObjects, observer *observe.Observer) *Pruner {
	return &Pruner{
		CommonObjects: common,
		observer:      observer,
		config:        newConfig(),
	}
}

// Ensure satisfies service.Handler
func (p *Pruner) Ensure(obj runtime.Object) error {

	prune, err := p.checkObject(obj)
	if err != nil {
		return err
	}

	if prune.Status.Started {
		return nil
	}

	pruneCopy := prune.DeepCopy()

	pruneCopy.GlobalOverrides = &backupv1alpha1.GlobalOverrides{}
	pruneCopy.GlobalOverrides.RegisteredBackend = service.MergeGlobalBackendConfig(pruneCopy.Spec.Backend, p.config.GlobalConfig)

	pruneRunner := newPruneRunner(p.CommonObjects, p.config, pruneCopy, p.observer)

	return pruneRunner.Start()

	return nil
}

// Delete satisfies service.Handler
func (p *Pruner) Delete(name string) error {
	return nil
}

func (p *Pruner) checkObject(obj runtime.Object) (*backupv1alpha1.Prune, error) {
	prune, ok := obj.(*backupv1alpha1.Prune)
	if !ok {
		return nil, fmt.Errorf("%v is not a check", obj.GetObjectKind())
	}
	return prune, nil
}
