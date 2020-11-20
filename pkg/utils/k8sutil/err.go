package k8sutil

import (
	"net/http"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// IsResourceNotFoundError return true if err cause is k8s resource not found
func IsResourceNotFoundError(err error) bool {
	if err == nil {
		return false
	}

	se, ok := err.(*apierrors.StatusError)
	if !ok {
		return false
	}
	if se.Status().Code == http.StatusNotFound && se.Status().Reason == metav1.StatusReasonNotFound {
		return true
	}
	return false
}
