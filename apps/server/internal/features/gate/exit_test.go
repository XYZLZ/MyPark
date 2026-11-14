package gate

import "testing"

// Every event type a device may send has a handler, so none answers 500.
func TestHandle_EveryTypeHandled(t *testing.T) {
	s := NewService(Deps{})
	for _, typ := range AllEventTypes() {
		if s.handlerFor(typ) == nil {
			t.Errorf("no handler for %s", typ)
		}
	}
}
