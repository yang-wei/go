// Copyright 2012 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file defines operands and associated operations.

package types2

import (
	"bytes"
	"cmd/compile/internal/syntax"
	"fmt"
	"go/constant"
	"go/token"
)

// An operandMode specifies the (addressing) mode of an operand.
type operandMode byte

const (
	invalid   operandMode = iota // operand is invalid
	novalue                      // operand represents no value (result of a function call w/o result)
	builtin                      // operand is a built-in function
	typexpr                      // operand is a type
	constant_                    // operand is a constant; the operand's typ is a Basic type
	variable                     // operand is an addressable variable
	mapindex                     // operand is a map index expression (acts like a variable on lhs, commaok on rhs of an assignment)
	value                        // operand is a computed value
	nilvalue                     // operand is the nil value
	commaok                      // like value, but operand may be used in a comma,ok expression
	commaerr                     // like commaok, but second value is error, not boolean
	cgofunc                      // operand is a cgo function
)

var operandModeString = [...]string{
	invalid:   "invalid operand",
	novalue:   "no value",
	builtin:   "built-in",
	typexpr:   "type",
	constant_: "constant",
	variable:  "variable",
	mapindex:  "map index expression",
	value:     "value",
	nilvalue:  "nil",
	commaok:   "comma, ok expression",
	commaerr:  "comma, error expression",
	cgofunc:   "cgo function",
}

// An operand represents an intermediate value during type checking.
// Operands have an (addressing) mode, the expression evaluating to
// the operand, the operand's type, a value for constants, and an id
// for built-in functions.
// The zero value of operand is a ready to use invalid operand.
//
type operand struct {
	mode operandMode
	expr syntax.Expr
	typ  Type
	val  constant.Value
	id   builtinId
}

// Pos returns the position of the expression corresponding to x.
// If x is invalid the position is nopos.
//
func (x *operand) Pos() syntax.Pos {
	// x.expr may not be set if x is invalid
	if x.expr == nil {
		return nopos
	}
	return x.expr.Pos()
}

// Operand string formats
// (not all "untyped" cases can appear due to the type system,
// but they fall out naturally here)
//
// mode       format
//
// invalid    <expr> (               <mode>                    )
// novalue    <expr> (               <mode>                    )
// builtin    <expr> (               <mode>                    )
// typexpr    <expr> (               <mode>                    )
//
// constant   <expr> (<untyped kind> <mode>                    )
// constant   <expr> (               <mode>       of type <typ>)
// constant   <expr> (<untyped kind> <mode> <val>              )
// constant   <expr> (               <mode> <val> of type <typ>)
//
// variable   <expr> (<untyped kind> <mode>                    )
// variable   <expr> (               <mode>       of type <typ>)
//
// mapindex   <expr> (<untyped kind> <mode>                    )
// mapindex   <expr> (               <mode>       of type <typ>)
//
// value      <expr> (<untyped kind> <mode>                    )
// value      <expr> (               <mode>       of type <typ>)
//
// nilvalue   untyped nil
// nilvalue   nil    (                            of type <typ>)
//
// commaok    <expr> (<untyped kind> <mode>                    )
// commaok    <expr> (               <mode>       of type <typ>)
//
// commaerr   <expr> (<untyped kind> <mode>                    )
// commaerr   <expr> (               <mode>       of type <typ>)
//
// cgofunc    <expr> (<untyped kind> <mode>                    )
// cgofunc    <expr> (               <mode>       of type <typ>)
//
func operandString(x *operand, qf Qualifier) string {
	// special-case nil
	if x.mode == nilvalue {
		switch x.typ {
		case nil, Typ[Invalid]:
			return "nil (with invalid type)"
		case Typ[UntypedNil]:
			return "untyped nil"
		default:
			return fmt.Sprintf("nil (of type %s)", TypeString(x.typ, qf))
		}
	}

	var buf bytes.Buffer

	var expr string
	if x.expr != nil {
		expr = syntax.String(x.expr)
	} else {
		switch x.mode {
		case builtin:
			expr = predeclaredFuncs[x.id].name
		case typexpr:
			expr = TypeString(x.typ, qf)
		case constant_:
			expr = x.val.String()
		}
	}

	// <expr> (
	if expr != "" {
		buf.WriteString(expr)
		buf.WriteString(" (")
	}

	// <untyped kind>
	hasType := false
	switch x.mode {
	case invalid, novalue, builtin, typexpr:
		// no type
	default:
		// should have a type, but be cautious (don't crash during printing)
		if x.typ != nil {
			if isUntyped(x.typ) {
				buf.WriteString(x.typ.(*Basic).name)
				buf.WriteByte(' ')
				break
			}
			hasType = true
		}
	}

	// <mode>
	buf.WriteString(operandModeString[x.mode])

	// <val>
	if x.mode == constant_ {
		if s := x.val.String(); s != expr {
			buf.WriteByte(' ')
			buf.WriteString(s)
		}
	}

	// <typ>
	if hasType {
		if x.typ != Typ[Invalid] {
			var intro string
			if isGeneric(x.typ) {
				intro = " of parameterized type "
			} else {
				intro = " of type "
			}
			buf.WriteString(intro)
			WriteType(&buf, x.typ, qf)
			if tpar := asTypeParam(x.typ); tpar != nil {
				buf.WriteString(" constrained by ")
				WriteType(&buf, tpar.bound, qf) // do not compute interface type sets here
			}
		} else {
			buf.WriteString(" with invalid type")
		}
	}

	// )
	if expr != "" {
		buf.WriteByte(')')
	}

	return buf.String()
}

func (x *operand) String() string {
	return operandString(x, nil)
}

// setConst sets x to the untyped constant for literal lit.
func (x *operand) setConst(k syntax.LitKind, lit string) {
	var kind BasicKind
	switch k {
	case syntax.IntLit:
		kind = UntypedInt
	case syntax.FloatLit:
		kind = UntypedFloat
	case syntax.ImagLit:
		kind = UntypedComplex
	case syntax.RuneLit:
		kind = UntypedRune
	case syntax.StringLit:
		kind = UntypedString
	default:
		unreachable()
	}

	val := constant.MakeFromLiteral(lit, kind2tok[k], 0)
	if val.Kind() == constant.Unknown {
		x.mode = invalid
		x.typ = Typ[Invalid]
		return
	}
	x.mode = constant_
	x.typ = Typ[kind]
	x.val = val
}

// isNil reports whether x is a typed or the untyped nil value.
func (x *operand) isNil() bool { return x.mode == nilvalue }

// assignableTo reports whether x is assignable to a variable of type T. If the
// result is false and a non-nil reason is provided, it may be set to a more
// detailed explanation of the failure (result != ""). The returned error code
// is only valid if the (first) result is false. The check parameter may be nil
// if assignableTo is invoked through an exported API call, i.e., when all
// methods have been type-checked.
func (x *operand) assignableTo(check *Checker, T Type, reason *string) (bool, errorCode) {
	if x.mode == invalid || T == Typ[Invalid] {
		return true, 0 // avoid spurious errors
	}

	V := x.typ

	const debugAssignableTo = false
	if debugAssignableTo && check != nil {
		check.dump("V = %s", V)
		check.dump("T = %s", T)
	}

	// x's type is identical to T
	if Identical(V, T) {
		return true, 0
	}

	Vu := optype(V)
	Tu := optype(T)

	if debugAssignableTo && check != nil {
		check.dump("Vu = %s", Vu)
		check.dump("Tu = %s", Tu)
	}

	// x is an untyped value representable by a value of type T.
	if isUntyped(Vu) {
		if t, ok := under(T).(*TypeParam); ok {
			return t.is(func(t *term) bool {
				// TODO(gri) this could probably be more efficient
				if t.tilde {
					// TODO(gri) We need to check assignability
					//           for the underlying type of x.
				}
				ok, _ := x.assignableTo(check, t.typ, reason)
				return ok
			}), _IncompatibleAssign
		}
		newType, _, _ := check.implicitTypeAndValue(x, Tu)
		return newType != nil, _IncompatibleAssign
	}
	// Vu is typed

	// x's type V and T have identical underlying types
	// and at least one of V or T is not a named type
	if Identical(Vu, Tu) && (!isNamed(V) || !isNamed(T)) {
		return true, 0
	}

	// T is an interface type and x implements T
	if Ti, ok := Tu.(*Interface); ok {
		if m, wrongType := check.missingMethod(V, Ti, true); m != nil /* Implements(V, Ti) */ {
			if reason != nil {
				// TODO(gri) the error messages here should follow the style in Checker.typeAssertion (factor!)
				if wrongType != nil {
					if Identical(m.typ, wrongType.typ) {
						*reason = fmt.Sprintf("missing method %s (%s has pointer receiver)", m.name, m.name)
					} else {
						*reason = fmt.Sprintf("wrong type for method %s (have %s, want %s)", m.Name(), wrongType.typ, m.typ)
					}

				} else {
					*reason = "missing method " + m.Name()
				}
			}
			return false, _InvalidIfaceAssign
		}
		return true, 0
	}

	// x is a bidirectional channel value, T is a channel
	// type, x's type V and T have identical element types,
	// and at least one of V or T is not a named type
	if Vc, ok := Vu.(*Chan); ok && Vc.dir == SendRecv {
		if Tc, ok := Tu.(*Chan); ok && Identical(Vc.elem, Tc.elem) {
			return !isNamed(V) || !isNamed(T), _InvalidChanAssign
		}
	}

	return false, _IncompatibleAssign
}

// kind2tok translates syntax.LitKinds into token.Tokens.
var kind2tok = [...]token.Token{
	syntax.IntLit:    token.INT,
	syntax.FloatLit:  token.FLOAT,
	syntax.ImagLit:   token.IMAG,
	syntax.RuneLit:   token.CHAR,
	syntax.StringLit: token.STRING,
}
