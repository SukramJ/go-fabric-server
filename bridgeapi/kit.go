// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridgeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/device"

	"github.com/SukramJ/go-fabric-server/protocol/schema"
)

// clusterKit is one typed cluster package of go-fabric
// (cluster/spec/<cluster>, imported as <cluster>def) as the bridge half
// drives it: a patch decoded into its Patch, a Declare built from it, its
// typed Server's Set and Get. kits (clusters_gen.go, generated
// by internal/gen) holds one per cluster.
type clusterKit interface {
	// cluster is the snapshot's cluster.
	cluster() *schema.Cluster
	// err is why the package does not fit the kit's reading of it; nil
	// when it does.
	err() error
	// decodePatch decodes a patch object into the package's Patch; path
	// names the object in an error.
	decodePatch(raw json.RawMessage, path string) (any, error)
	// declare is the package's Declare: the patch as its Initial, the
	// feature selection, and the host's hooks.
	declare(patch any, features uint32, h *hooks) (device.Declaration, error)
	// set applies a patch through the typed server's Set.
	set(srv contract.ClusterServer, patch any) error
	// state is the typed server's state in wire form: served attributes
	// by property name.
	state(srv contract.ClusterServer) map[string]any
	// hasFeatures reports whether the Declare takes a feature selection.
	hasFeatures() bool
	// handles reports whether the Declare has a handler field for the
	// command.
	handles(cmd uint32) bool
	// patchAttributes are the attribute ids the Patch sets, ascending.
	patchAttributes() []uint32
}

// typedServer is the method set of a generated <cluster>def.Server the
// kit uses. Changes are followed through device.Device.OnChangedAsync,
// on the device's own goroutine, not through the typed OnChanged, which
// runs inline in the server's change.
type typedServer[P, S any] interface {
	Set(p P) error
	Get() S
}

// kit is the [clusterKit] of one package: P its Patch, S its State, D its
// Declare, T its *Server. The layout of P, S and D is read once, by
// reflection: the attribute each Patch field sets (from the Patch's own
// AttributeValues), the State field of the same name, and the Declare's
// Initial, Features, OnWrite and Handle<Command> fields.
type kit[P, S any, D device.Declaration, T typedServer[P, S]] struct {
	cl   *schema.Cluster
	wrap func(contract.ClusterServer) T
	bad  error

	patchField map[uint32]int    // attribute → Patch field index
	stateField map[uint32]int    // attribute → State field index
	byKey      map[string]uint32 // lower-cased Go field and property name → attribute
	nullField  int               // the Patch's Null field, -1 when it has none
	attrs      []uint32

	initialField  int
	featuresField int // -1 when the Declare takes no selection
	onWriteField  int // -1 when it takes no veto
	handlers      map[uint32]int
}

func newKit[P, S any, D device.Declaration, T typedServer[P, S]](name string, wrap func(contract.ClusterServer) T) clusterKit {
	k := &kit[P, S, D, T]{
		wrap: wrap, patchField: map[uint32]int{}, stateField: map[uint32]int{}, byKey: map[string]uint32{},
		nullField: -1, initialField: -1, featuresField: -1, onWriteField: -1, handlers: map[uint32]int{},
	}
	k.cl = schema.ClusterByName(name)
	if k.cl == nil {
		k.bad = fmt.Errorf("cluster %s is not in the snapshot", name)
		return k
	}
	k.bad = k.read()
	return k
}

func (k *kit[P, S, D, T]) read() error {
	type valuer interface{ AttributeValues() map[uint32]any }
	pt := reflect.TypeFor[P]()
	for i := range pt.NumField() {
		f := pt.Field(i)
		if f.Name == "Null" {
			k.nullField = i
			continue
		}
		if f.Type.Kind() != reflect.Pointer {
			return fmt.Errorf("%s: Patch field %s is not a pointer", k.cl.Name, f.Name)
		}
		var p P
		reflect.ValueOf(&p).Elem().Field(i).Set(reflect.New(f.Type.Elem()))
		v, ok := any(p).(valuer)
		if !ok {
			return fmt.Errorf("%s: Patch has no AttributeValues", k.cl.Name)
		}
		vals := v.AttributeValues()
		if len(vals) != 1 {
			return fmt.Errorf("%s: Patch field %s sets %d attributes", k.cl.Name, f.Name, len(vals))
		}
		for id := range vals {
			k.patchField[id] = i
			k.attrs = append(k.attrs, id)
			k.byKey[strings.ToLower(f.Name)] = id
			if a := k.cl.Attribute(id); a != nil {
				k.byKey[strings.ToLower(a.Property)] = id
			}
			sf, ok := reflect.TypeFor[S]().FieldByName(f.Name)
			if !ok || len(sf.Index) != 1 {
				return fmt.Errorf("%s: State has no field %s", k.cl.Name, f.Name)
			}
			k.stateField[id] = sf.Index[0]
		}
	}
	slices.Sort(k.attrs)
	dt := reflect.TypeFor[D]()
	for i := range dt.NumField() {
		f := dt.Field(i)
		switch {
		case f.Name == "Initial" && f.Type == pt:
			k.initialField = i
		case f.Name == "Features" && f.Type.Kind() == reflect.Uint32:
			k.featuresField = i
		case f.Name == "OnWrite" && f.Type == reflect.TypeFor[func(context.Context, S, uint32) error]():
			k.onWriteField = i
		case strings.HasPrefix(f.Name, "Handle") && f.Type.Kind() == reflect.Func:
			ft := f.Type
			if ft.NumIn() != 2 || ft.NumOut() != 2 || ft.Out(1) != reflect.TypeFor[error]() {
				return fmt.Errorf("%s: %s is not func(ctx, req) (resp, error)", k.cl.Name, f.Name)
			}
			// A handler of a command the snapshot does not state as a
			// request of this cluster (one its conformance disallows,
			// MicrowaveOvenMode's ChangeToMode) is not offered.
			if cmd := commandNamed(k.cl, strings.TrimPrefix(f.Name, "Handle")); cmd != nil {
				k.handlers[cmd.ID] = i
			}
		}
	}
	if k.initialField < 0 {
		return fmt.Errorf("%s: Declare has no Initial", k.cl.Name)
	}
	return nil
}

// commandNamed is the request command whose name, as a Go identifier,
// is goName.
func commandNamed(cl *schema.Cluster, goName string) *schema.Command {
	for i := range cl.Commands {
		c := &cl.Commands[i]
		if c.Request && strings.EqualFold(identifier(c.Name), goName) {
			return c
		}
	}
	return nil
}

func identifier(name string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1
	}, name)
}

func (k *kit[P, S, D, T]) cluster() *schema.Cluster  { return k.cl }
func (k *kit[P, S, D, T]) err() error                { return k.bad }
func (k *kit[P, S, D, T]) hasFeatures() bool         { return k.featuresField >= 0 }
func (k *kit[P, S, D, T]) patchAttributes() []uint32 { return k.attrs }

func (k *kit[P, S, D, T]) handles(cmd uint32) bool {
	_, ok := k.handlers[cmd]
	return ok
}

// attribute resolves a patch key: a property name or the Go field name,
// without regard to case.
func (k *kit[P, S, D, T]) attribute(key string) (uint32, bool) {
	id, ok := k.byKey[strings.ToLower(key)]
	return id, ok
}

func (k *kit[P, S, D, T]) decodePatch(raw json.RawMessage, path string) (any, error) {
	var p P
	if t := bytes.TrimSpace(raw); len(t) == 0 || string(t) == "null" {
		return p, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("%s: a patch is an object of attributes by name", path)
	}
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	pv := reflect.ValueOf(&p).Elem()
	var nulls []uint32
	seen := map[uint32]string{}
	for _, key := range keys {
		at := path + "." + key
		id, ok := k.attribute(key)
		if !ok {
			return nil, fmt.Errorf("%s: the %s cluster has no attribute %q", at, k.cl.Name, key)
		}
		if prev, dup := seen[id]; dup {
			return nil, fmt.Errorf("%s: the attribute is also given as %q", at, prev)
		}
		seen[id] = key
		val := bytes.TrimSpace(m[key])
		if string(val) == "null" {
			if a := k.cl.Attribute(id); a == nil || !a.Nullable || k.nullField < 0 {
				return nil, fmt.Errorf("%s: the attribute is not nullable", at)
			}
			nulls = append(nulls, id)
			continue
		}
		f := pv.Field(k.patchField[id])
		ptr := reflect.New(f.Type().Elem())
		dec := json.NewDecoder(bytes.NewReader(val))
		dec.DisallowUnknownFields()
		if err := dec.Decode(ptr.Interface()); err != nil {
			return nil, fmt.Errorf("%s: %s", at, decodeError(err))
		}
		f.Set(ptr)
	}
	if len(nulls) > 0 {
		slices.Sort(nulls)
		pv.Field(k.nullField).Set(reflect.ValueOf(nulls))
	}
	return p, nil
}

// decodeError words a decoding failure without encoding/json's "json:"
// prefix and with the Go type in the protocol's terms.
func decodeError(err error) string {
	var te *json.UnmarshalTypeError
	if errors.As(err, &te) {
		if te.Field != "" {
			return fmt.Sprintf("member %s: %s is not a valid %s", te.Field, te.Value, kindName(te.Type))
		}
		return fmt.Sprintf("%s is not a valid %s", te.Value, kindName(te.Type))
	}
	return strings.TrimPrefix(err.Error(), "json: ")
}

func kindName(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Bool:
		return "boolean"
	case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return fmt.Sprintf("unsigned %d-bit integer", t.Bits())
	case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return fmt.Sprintf("signed %d-bit integer", t.Bits())
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.String:
		return "string"
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return "base64 string"
		}
		return "list"
	case reflect.Struct:
		return "object"
	}
	return t.String()
}

func (k *kit[P, S, D, T]) declare(patch any, features uint32, h *hooks) (device.Declaration, error) {
	p, ok := patch.(P)
	if !ok {
		return nil, fmt.Errorf("%s: the patch is a %T", k.cl.Name, patch)
	}
	var d D
	dv := reflect.ValueOf(&d).Elem()
	dv.Field(k.initialField).Set(reflect.ValueOf(p))
	if features != 0 {
		if k.featuresField < 0 {
			return nil, fmt.Errorf("the %s cluster takes no feature selection", k.cl.Name)
		}
		dv.Field(k.featuresField).SetUint(uint64(features))
	}
	if h == nil {
		return d, nil
	}
	if h.writes {
		if k.onWriteField < 0 {
			return nil, fmt.Errorf("the %s cluster takes no write veto", k.cl.Name)
		}
		veto := func(ctx context.Context, next S, attrID uint32) error {
			return h.write(ctx, attrID, k.attributeValue(next, attrID))
		}
		dv.Field(k.onWriteField).Set(reflect.ValueOf(veto))
	}
	for _, cmd := range h.commands {
		idx, ok := k.handlers[cmd]
		if !ok {
			return nil, fmt.Errorf("the %s cluster has no host handler for command 0x%02X", k.cl.Name, cmd)
		}
		ft := dv.Field(idx).Type()
		respType := ft.Out(0)
		fields := k.cl.Command(cmd).Fields
		fn := reflect.MakeFunc(ft, func(args []reflect.Value) []reflect.Value {
			ctx, _ := args[0].Interface().(context.Context)
			resp := reflect.New(respType)
			err := h.command(ctx, cmd, wireFields(fields, args[1].Interface()), func(raw json.RawMessage) error {
				return decodeResponse(raw, resp.Interface())
			})
			out := []reflect.Value{resp.Elem(), reflect.Zero(reflect.TypeFor[error]())}
			if err != nil {
				out[0] = reflect.Zero(respType)
				out[1] = reflect.ValueOf(&err).Elem()
			}
			return out
		})
		dv.Field(idx).Set(fn)
	}
	return d, nil
}

// decodeResponse decodes a host's command response into the command's
// response type: members by field name without regard to case, an
// unknown member refused.
func decodeResponse(raw json.RawMessage, dst any) error {
	if t := bytes.TrimSpace(raw); len(t) == 0 || string(t) == "null" {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("response: %s", decodeError(err))
	}
	return nil
}

func (k *kit[P, S, D, T]) set(srv contract.ClusterServer, patch any) error {
	p, ok := patch.(P)
	if !ok {
		return fmt.Errorf("%s: the patch is a %T", k.cl.Name, patch)
	}
	return k.wrap(srv).Set(p)
}

func (k *kit[P, S, D, T]) state(srv contract.ClusterServer) map[string]any {
	return k.wireState(k.wrap(srv).Get(), served(srv))
}

// served is the attributes srv serves, nil when it does not say.
func served(srv contract.ClusterServer) map[uint32]bool {
	l, ok := srv.(contract.ClusterAttributeLister)
	if !ok {
		return nil
	}
	m := map[uint32]bool{}
	for _, id := range l.MatterAttributes() {
		m[id] = true
	}
	return m
}

// wireState is a State in wire form: each attribute the server serves
// (every one when only is nil), by property name.
func (k *kit[P, S, D, T]) wireState(st S, only map[uint32]bool) map[string]any {
	out := make(map[string]any, len(k.attrs))
	for _, id := range k.attrs {
		if only != nil && !only[id] {
			continue
		}
		a := k.cl.Attribute(id)
		if a == nil {
			continue
		}
		out[a.Property] = k.attributeValue(st, id)
	}
	return out
}

// attributeValue is one attribute of a State in wire form.
func (k *kit[P, S, D, T]) attributeValue(st S, id uint32) any {
	idx, ok := k.stateField[id]
	if !ok {
		return nil
	}
	v := reflect.ValueOf(st).Field(idx).Interface()
	if a := k.cl.Attribute(id); a != nil {
		return toWire(a.Type, v)
	}
	return toWire(schema.Type{}, v)
}

// wireFields is a command request in wire form: its fields by property
// name.
func wireFields(fields []schema.Field, req any) any {
	return toWire(schema.Type{Meta: schema.MetaStruct, Struct: &schema.Struct{Fields: fields}}, req)
}

// toWire is a Go value of a typed package in the protocol's JSON form:
// encoding/json's (numbers, base64 bytes, null for a nil pointer), with
// struct members renamed from their Go name to the snapshot's property
// name.
func toWire(t schema.Type, v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var out any
	if err := dec.Decode(&out); err != nil {
		return nil
	}
	return rename(t, out)
}

func rename(t schema.Type, v any) any {
	switch t.Meta {
	case schema.MetaStruct:
		m, ok := v.(map[string]any)
		if !ok || t.Struct == nil {
			return v
		}
		out := make(map[string]any, len(m))
		for key, val := range m {
			f := memberOf(t.Struct, key)
			if f == nil {
				out[key] = val
				continue
			}
			out[f.Property] = rename(f.Type, val)
		}
		return out
	case schema.MetaList:
		arr, ok := v.([]any)
		if !ok || t.Entry == nil {
			return v
		}
		for i := range arr {
			arr[i] = rename(*t.Entry, arr[i])
		}
		return arr
	default:
		return v
	}
}

func memberOf(s *schema.Struct, key string) *schema.Field {
	for i := range s.Fields {
		f := &s.Fields[i]
		if strings.EqualFold(key, identifier(f.Name)) || strings.EqualFold(key, f.Property) {
			return f
		}
	}
	return nil
}

// kitByName is the kit of a cluster name, without regard to case.
func kitByName(name string) (clusterKit, error) {
	cl := schema.ClusterByName(name)
	if cl == nil {
		return nil, fmt.Errorf("no cluster %q", name)
	}
	k, ok := kits[cl.ID]
	if !ok {
		return nil, fmt.Errorf("the %s cluster has no typed package in go-fabric and cannot be declared", cl.Name)
	}
	if err := k.err(); err != nil {
		return nil, err
	}
	return k, nil
}
