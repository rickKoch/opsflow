package actor

import "sync"

// registry manages a thread-safe map of actor logic.
type registry struct {
	actors sync.Map // map[string]ActorFunc
}

func (ar *registry) register(name string, fn ActorFunc) {
	ar.actors.Store(name, fn)
}

func (ar *registry) registerMany(regs map[string]ActorFunc) {
	for name, fn := range regs {
		ar.register(name, fn)
	}
}

func (ar *registry) get(name string) (ActorFunc, bool) {
	fn, ok := ar.actors.Load(name)
	if !ok {
		return nil, false
	}
	return fn.(ActorFunc), true
}

func (ar *registry) exist(name string) bool {
	_, ok := ar.actors.Load(name)
	return ok
}
