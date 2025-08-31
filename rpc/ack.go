package rpc

// NotifyAcks notifies engine about received acknowledgements.
func (e *Engine) NotifyAcks(ids []int64) {
	e.mux.Lock()
	defer e.mux.Unlock()

	for _, id := range ids {
		ch, ok := e.ack[id]
		if !ok {
			e.log.Debug().Int64("msg_id", id).Msg("Acknowledge callback not set")
			continue
		}

		close(ch)
		delete(e.ack, id)
	}
}

func (e *Engine) waitAck(id int64) chan struct{} {
	e.mux.Lock()
	defer e.mux.Unlock()

	log := e.log.With().Int64("ack_id", id).Logger()
	if c, found := e.ack[id]; found {
		log.Warn().Msg("Ack already registered")
		return c
	}

	log.Debug().Msg("Waiting for acknowledge")
	c := make(chan struct{})
	e.ack[id] = c
	return c
}

func (e *Engine) removeAck(id int64) {
	e.mux.Lock()
	defer e.mux.Unlock()

	delete(e.ack, id)
}
