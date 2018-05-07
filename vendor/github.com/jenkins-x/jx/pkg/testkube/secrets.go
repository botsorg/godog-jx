package testkube

import (
	"github.com/jenkins-x/jx/pkg/kube"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CreateTestPipelineGitSecret creates a test git pipeline credential secret
func CreateTestPipelineGitSecret(gitServiceKind string, name string, gitUrl string, username string, password string) corev1.Secret {
	return corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: kube.ToValidName(name),
			Annotations: map[string]string{
				kube.AnnotationURL:  gitUrl,
				kube.AnnotationName: name,
			},
			Labels: map[string]string{
				kube.LabelKind:            kube.ValueKindGit,
				kube.LabelCredentialsType: kube.ValueCredentialTypeUsernamePassword,
				kube.LabelServiceKind:     gitServiceKind,
			},
		},
		Data: map[string][]byte{
			kube.SecretDataUsername: []byte(username),
			kube.SecretDataPassword: []byte(password),
		},
	}
}
