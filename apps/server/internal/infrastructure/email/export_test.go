package email

// Build exposes the message rendering to the external tests.
func (s *SMTP) Build(m Message) (from, to string, body []byte, err error) { return s.build(m) }
