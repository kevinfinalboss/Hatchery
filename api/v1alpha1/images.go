package v1alpha1

func (e *Egg) ResolveImage(name string) (image string, ok bool) {
	if len(e.Spec.Images) == 0 {
		return "", false
	}
	if name == "" {
		return e.Spec.Images[0].Image, true
	}
	for _, img := range e.Spec.Images {
		if img.Name == name {
			return img.Image, true
		}
	}
	return "", false
}
