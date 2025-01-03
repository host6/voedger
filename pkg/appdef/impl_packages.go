/*
 * Copyright (c) 2024-present Sigma-Soft, Ltd.
 * @author: Nikolay Nikitin
 */

package appdef

import (
	"iter"
	"slices"
)

type packages struct {
	local       []string
	localByPath map[string]string
	pathByLocal map[string]string
}

func newPackages() *packages {
	return &packages{
		local:       make([]string, 0),
		localByPath: make(map[string]string),
		pathByLocal: make(map[string]string),
	}
}

func (p *packages) add(local, path string) {
	if ok, err := ValidIdent(local); !ok {
		panic(err)
	}
	if p, ok := p.pathByLocal[local]; ok {
		panic(ErrAlreadyExists("package local name «%s» already used for «%s»", local, p))
	}

	if path == "" {
		panic(ErrMissed("package «%s» path", local))
	}
	if l, ok := p.localByPath[path]; ok {
		panic(ErrAlreadyExists("package path «%s» already used for «%s»", path, l))
	}

	p.local = append(p.local, local)
	slices.Sort(p.local)

	p.localByPath[path] = local
	p.pathByLocal[local] = path
}

func (p packages) all() iter.Seq2[string, string] {
	return func(visit func(string, string) bool) {
		for _, local := range p.local {
			if !visit(local, p.pathByLocal[local]) {
				break
			}
		}
	}
}

func (p packages) fullQName(n QName) FullQName {
	if path, ok := p.pathByLocal[n.Pkg()]; ok {
		return NewFullQName(path, n.Entity())
	}
	return NullFullQName
}

func (p packages) localNameByPath(path string) string {
	return p.localByPath[path]
}

func (p packages) pathByLocalName(local string) string {
	return p.pathByLocal[local]
}

func (p packages) localQName(n FullQName) QName {
	if pkg, ok := p.localByPath[n.PkgPath()]; ok {
		return NewQName(pkg, n.Entity())
	}
	return NullQName
}
