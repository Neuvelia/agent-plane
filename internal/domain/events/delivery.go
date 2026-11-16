package events

// DeliveryStats counts encoded whole-event JSON bytes, not Go heap usage.
type DeliveryStats struct {
	RetainedPeakBytes, RetainedPeakItems        int
	LivePeakBytes, LivePeakItems                int
	RetainedOmittedEvents, RetainedOmittedBytes uint64
	LiveOmittedEvents, LiveOmittedBytes         uint64
	LiveStopped                                 bool
}
