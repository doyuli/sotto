package macnative

func activationChanged(source, target, current, self int32) bool {
	return target > 0 && target != source && target != self && target == current
}
