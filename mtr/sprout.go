package mtr

import "github.com/go-sprout/sprout"

type SproutReg struct {
	uid string
	hn  sprout.Handler
	fns sprout.FunctionMap
}

func NewSproutRegistry(uid string, fns sprout.FunctionMap) sprout.Registry {
	return &SproutReg{uid: uid, fns: fns}
}

func (s *SproutReg) Uid() string {
	return s.uid
}

func (s *SproutReg) LinkHandler(hn sprout.Handler) error {
	s.hn = hn
	return nil
}

func (s *SproutReg) RegisterFunctions(fm sprout.FunctionMap) error {
	for name, fn := range s.fns {
		sprout.AddFunction(fm, name, fn)
	}
	return nil
}

func (s *SproutReg) RegisterAliases(am sprout.FunctionAliasMap) error {
	return nil
}
