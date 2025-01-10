package kmsg

import "k8s.io/klog/v2"

// set up a logger that works with kmsgparser's Logger interface,
// so we can bubble up logs to klog from the parser
type kmsgParserLogger struct{}

func (k *kmsgParserLogger) Warningf(fmt string, args ...interface{}) {
	klog.Warningf(fmt, args...)
}

func (k *kmsgParserLogger) Infof(fmt string, args ...interface{}) {
	klog.Infof(fmt, args...)
}

func (k *kmsgParserLogger) Errorf(fmt string, args ...interface{}) {
	klog.Errorf(fmt, args...)
}
