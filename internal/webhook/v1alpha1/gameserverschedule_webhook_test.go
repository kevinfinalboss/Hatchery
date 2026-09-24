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

package v1alpha1

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

var _ = Describe("GameServerSchedule Webhook", func() {
	schedule := func(name, server, cron string) *gameserversv1alpha1.GameServerSchedule {
		return &gameserversv1alpha1.GameServerSchedule{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: gameserversv1alpha1.GameServerScheduleSpec{
				GameServerRef: gameserversv1alpha1.GameServerRef{Name: server}, Cron: cron, OnlyWhenRunning: true,
				Tasks: []gameserversv1alpha1.ScheduleTask{{Action: gameserversv1alpha1.ScheduleActionRestart}},
			},
		}
	}

	It("rejects a schedule for a GameServer that does not exist", func() {
		err := k8sClient.Create(ctx, schedule("ghost-sched", "ghost", "0 4 * * *"))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("ghost"))
	})

	It("rejects an invalid cron with the validator's message", func() {
		egg := &gameserversv1alpha1.Egg{
			ObjectMeta: metav1.ObjectMeta{Name: "sched-target-egg", Namespace: "default"},
			Spec: gameserversv1alpha1.EggSpec{
				Images:       []gameserversv1alpha1.EggImage{{Name: "default", Image: "example.com/game:latest"}},
				StartCommand: "start",
			},
		}
		Expect(k8sClient.Create(ctx, egg)).To(Succeed())
		Expect(k8sClient.Create(ctx, &gameserversv1alpha1.GameServer{
			ObjectMeta: metav1.ObjectMeta{Name: "sched-target", Namespace: "default"},
			Spec: gameserversv1alpha1.GameServerSpec{
				EggRef:  gameserversv1alpha1.GameServerEggRef{Name: egg.Name},
				Storage: gameserversv1alpha1.GameServerStorage{Size: "1Gi"},
			},
		})).To(Succeed())
		err := k8sClient.Create(ctx, schedule("bad-cron", "sched-target", "* * * * *"))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("at most every"))
	})

	It("admits a valid schedule", func() {
		Expect(k8sClient.Create(ctx, schedule("good", "sched-target", "0 4 * * *"))).To(Succeed())
	})
})
