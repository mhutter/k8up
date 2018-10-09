package prune

import (
	"strconv"
	"strings"

	backupv1alpha1 "git.vshn.net/vshn/baas/apis/backup/v1alpha1"
	"git.vshn.net/vshn/baas/service"
	"git.vshn.net/vshn/baas/service/observe"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

type pruneRunner struct {
	service.CommonObjects
	config   config
	observer *observe.Observer
	prune    *backupv1alpha1.Prune
}

func newPruneRunner(common service.CommonObjects, config config, prune *backupv1alpha1.Prune, observer *observe.Observer) *pruneRunner {
	return &pruneRunner{
		CommonObjects: common,
		config:        config,
		observer:      observer,
		prune:         prune,
	}
}

func (p *pruneRunner) Stop() error                         { return nil }
func (p *pruneRunner) SameSpec(object runtime.Object) bool { return true }

func (p *pruneRunner) Start() error {
	p.Logger.Infof("New prune job received %v in namespace %v", p.prune.Name, p.prune.Namespace)
	p.prune.Status.Started = true
	p.updatePruneStatus()

	pruneJob := p.newPruneJob(p.prune, p.config)

	go p.watchState(pruneJob)

	_, err := p.K8sCli.Batch().Jobs(p.prune.Namespace).Create(pruneJob)
	if err != nil {
		return err
	}

	return nil
}

func (p *pruneRunner) newPruneJob(prune *backupv1alpha1.Prune, config config) *batchv1.Job {
	job := service.GetBasicJob("prune", p.config.GlobalConfig, &p.prune.ObjectMeta)

	job.Spec.Template.Spec.Containers[0].Args = []string{"-prune"}

	envVar := p.setUpRetention(p.prune)

	envVar = append(envVar, service.BuildRepoPasswordVar(prune.GlobalOverrides.RegisteredBackend.RepoPasswordSecretRef, config.GlobalConfig))

	envVar = append(envVar, service.BuildS3EnvVars(prune.GlobalOverrides.RegisteredBackend.S3, config.GlobalConfig)...)

	job.Spec.Template.Spec.Containers[0].Env = append(envVar, job.Spec.Template.Spec.Containers[0].Env...)

	return job
}

func (p *pruneRunner) updatePruneStatus() {
	// Just overwrite the resource
	result, err := p.BaasCLI.AppuioV1alpha1().Prunes(p.prune.Namespace).Get(p.prune.Name, metav1.GetOptions{})
	if err != nil {
		p.Logger.Errorf("Cannot get baas object: %v", err)
	}

	result.Status = p.prune.Status
	_, updateErr := p.BaasCLI.AppuioV1alpha1().Prunes(p.prune.Namespace).Update(result)
	if updateErr != nil {
		p.Logger.Errorf("Coud not update prune resource: %v", updateErr)
	}
}

func (p *pruneRunner) setUpRetention(prune *backupv1alpha1.Prune) []corev1.EnvVar {
	retentionRules := []corev1.EnvVar{}

	if prune.Spec.Retention.KeepLast > 0 {
		retentionRules = append(retentionRules, corev1.EnvVar{
			Name:  string(service.KeepLast),
			Value: strconv.Itoa(prune.Spec.Retention.KeepLast),
		})
	}

	if prune.Spec.Retention.KeepHourly > 0 {
		retentionRules = append(retentionRules, corev1.EnvVar{
			Name:  string(service.KeepHourly),
			Value: strconv.Itoa(prune.Spec.Retention.KeepHourly),
		})
	}

	if prune.Spec.Retention.KeepDaily > 0 {
		retentionRules = append(retentionRules, corev1.EnvVar{
			Name:  string(service.KeepDaily),
			Value: strconv.Itoa(prune.Spec.Retention.KeepDaily),
		})
	} else {
		//Set defaults
		retentionRules = append(retentionRules, corev1.EnvVar{
			Name:  string(service.KeepDaily),
			Value: strconv.Itoa(14),
		})
	}

	if prune.Spec.Retention.KeepWeekly > 0 {
		retentionRules = append(retentionRules, corev1.EnvVar{
			Name:  string(service.KeepWeekly),
			Value: strconv.Itoa(prune.Spec.Retention.KeepWeekly),
		})
	}

	if prune.Spec.Retention.KeepMonthly > 0 {
		retentionRules = append(retentionRules, corev1.EnvVar{
			Name:  string(service.KeepMonthly),
			Value: strconv.Itoa(prune.Spec.Retention.KeepMonthly),
		})
	}

	if prune.Spec.Retention.KeepYearly > 0 {
		retentionRules = append(retentionRules, corev1.EnvVar{
			Name:  string(service.KeepYearly),
			Value: strconv.Itoa(prune.Spec.Retention.KeepYearly),
		})
	}

	if len(prune.Spec.Retention.KeepTags) > 0 {
		retentionRules = append(retentionRules, corev1.EnvVar{
			Name:  string(service.KeepTag),
			Value: strings.Join(prune.Spec.Retention.KeepTags, ","),
		})
	}

	return retentionRules
}

func (p *pruneRunner) watchState(job *batchv1.Job) {
	subscription, err := p.observer.GetBroker().Subscribe(job.Labels[p.config.Identifier])
	if err != nil {
		p.Logger.Errorf("Cannot watch state of prune %v", p.prune.Name)
	}

	watch := observe.WatchObjects{
		Job:     job,
		JobType: observe.PruneType,
		Locker:  p.observer.GetLocker(),
		Logger:  p.Logger,
		Failedfunc: func(message observe.PodState) {
			p.prune.Status.Failed = true
			p.prune.Status.Finished = true
			p.updatePruneStatus()
		},
		Successfunc: func(message observe.PodState) {
			p.prune.Status.Finished = true
			p.updatePruneStatus()
		},
	}

	subscription.WatchLoop(watch)
}
