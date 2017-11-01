// =================================================================================================
// WARNING! This file is auto-generated. Do not hand-edit. Instead, edit interpreterRun.got
// and rerun generate.sh.
// =================================================================================================

package interpreter

import (
	"errors"
	"fmt"
	"math"
	"time"

	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/il"
)

func (in *Interpreter) run(fn *il.Function, bag attribute.Bag, step bool) (Result, error) {

	var registers [registerCount]uint32
	var sp uint32
	var ip uint32
	var fp uint32

	var opstack []uint32
	var frames []stackFrame

	var heap []interface{}
	var hp uint32

	strings := in.program.Strings()
	body := in.code

	var code uint32
	var t1 uint32
	var t2 uint32
	var t3 uint32
	var ti64 int64
	var tu64 uint64
	var tf64 float64
	var tVal interface{}
	var tStr string
	var tDur time.Duration
	var tBool bool
	var tFound bool
	var tErr error

	opstack = make([]uint32, opStackSize)
	frames = make([]stackFrame, callStackSize)
	heap = make([]interface{}, heapSize)
	ip = fn.Address

	if len(fn.Parameters) != 0 {
		tErr = errors.New("init function must have 0 args")
		goto RETURN_ERR
	}

	if step {
		copy(registers[:], in.stepper.registers[:])
		sp = in.stepper.sp
		ip = in.stepper.ip
		fp = in.stepper.fp
		copy(opstack, in.stepper.opstack)
		copy(frames, in.stepper.frames)
		copy(heap, in.stepper.heap)
		hp = in.stepper.hp
	}

	for {
		code = body[ip]
		ip++
		switch il.Opcode(code) {

		case il.Halt:
			tErr = errors.New("catching fire as instructed")
			goto RETURN_ERR

		case il.Nop:

		case il.Err:
			t1 = body[ip]
			ip++
			tStr = strings.GetString(t1)
			tErr = errors.New(tStr)
			goto RETURN_ERR

		case il.Errz:
			if sp < 1 {
				goto STACK_UNDERFLOW
			}
			t1 = body[ip]
			ip++
			sp--
			t2 = opstack[sp]
			if t2 == 0 {
				tStr = strings.GetString(t1)
				tErr = errors.New(tStr)
				goto RETURN_ERR
			}

		case il.Errnz:
			if sp < 1 {
				goto STACK_UNDERFLOW
			}
			t1 = body[ip]
			ip++
			sp--
			t2 = opstack[sp]
			if t2 != 0 {
				tStr = strings.GetString(t1)
				tErr = errors.New(tStr)
				goto RETURN_ERR
			}

		case il.PopS, il.PopB:
			if sp < 1 {
				goto STACK_UNDERFLOW
			}
			sp--

		case il.PopI, il.PopD:
			if sp < 2 {
				goto STACK_UNDERFLOW
			}
			sp = sp - 2

		case il.DupS, il.DupB:
			if sp < 1 {
				goto STACK_UNDERFLOW
			}
			if sp > opStackSize-1 {
				goto STACK_OVERFLOW
			}
			opstack[sp] = opstack[sp-1]
			sp++

		case il.DupI, il.DupD:
			if sp < 2 {
				goto STACK_UNDERFLOW
			}
			if sp > opStackSize-2 {
				goto STACK_OVERFLOW
			}
			opstack[sp] = opstack[sp-2]
			opstack[sp+1] = opstack[sp-1]
			sp += 2

		case il.RLoadS, il.RLoadB:
			if sp < 1 {
				goto STACK_UNDERFLOW
			}
			t1 = body[ip]
			ip++
			sp--
			registers[t1] = opstack[sp]

		case il.RLoadI, il.RLoadD:
			if sp < 2 {
				goto STACK_UNDERFLOW
			}
			t1 = body[ip]
			ip++
			registers[t1] = opstack[sp-1]
			registers[t1+1] = opstack[sp-2]
			sp = sp - 2

		case il.ALoadS, il.ALoadB:
			t1 = body[ip]
			ip++
			registers[t1] = body[ip]
			ip++

		case il.ALoadI, il.ALoadD:
			t1 = body[ip]
			ip++
			registers[t1] = body[ip]
			registers[t1+1] = body[ip+1]
			ip = ip + 2

		case il.RPushS, il.RPushB:
			t1 = body[ip]
			ip++
			if sp > opStackSize-1 {
				goto STACK_OVERFLOW
			}
			opstack[sp] = registers[t1]
			sp++

		case il.RPushI, il.RPushD:
			t1 = body[ip]
			ip++
			if sp > opStackSize-1 {
				goto STACK_OVERFLOW
			}
			opstack[sp] = registers[t1+1]
			opstack[sp+1] = registers[t1]
			sp = sp + 2

		case il.APushS, il.APushB:
			t1 = body[ip]
			ip++
			if sp > opStackSize-1 {
				goto STACK_OVERFLOW
			}
			opstack[sp] = t1
			sp++

		case il.APushI, il.APushD:
			t1 = body[ip]
			t2 = body[ip+1]
			ip = ip + 2
			if sp > opStackSize-2 {
				goto STACK_OVERFLOW
			}
			opstack[sp] = t2
			opstack[sp+1] = t1
			sp = sp + 2

		case il.EqS, il.EqB:
			if sp < 2 {
				goto STACK_UNDERFLOW
			}
			t1 = opstack[sp-1]
			t2 = opstack[sp-2]
			sp = sp - 2
			if t1 == t2 {
				opstack[sp] = 1
				sp++
			} else {
				opstack[sp] = 0
				sp++
			}

		case il.EqI, il.EqD:
			if sp < 4 {
				goto STACK_UNDERFLOW
			}
			if opstack[sp-1] == opstack[sp-3] && opstack[sp-2] == opstack[sp-4] {
				sp -= 4
				opstack[sp] = 1
				sp++
			} else {
				sp -= 4
				opstack[sp] = 0
				sp++
			}

		case il.AEqS, il.AEqB:
			t1 = body[ip]
			ip++
			if sp < 1 {
				goto STACK_UNDERFLOW
			}
			sp--
			t2 = opstack[sp]
			if t1 == t2 {
				opstack[sp] = 1
				sp++
			} else {
				opstack[sp] = 0
				sp++
			}

		case il.AEqI, il.AEqD:
			t1 = body[ip]
			t2 = body[ip+1]
			ip = ip + 2
			if sp < 2 {
				goto STACK_UNDERFLOW
			}
			if opstack[sp-1] == t1 && opstack[sp-2] == t2 {
				sp = sp - 2
				opstack[sp] = 1
				sp++
			} else {
				sp = sp - 2
				opstack[sp] = 0
				sp++
			}

		case il.Xor:
			if sp < 2 {
				goto STACK_UNDERFLOW
			}
			t1 = opstack[sp-1]
			t2 = opstack[sp-2]
			sp = sp - 2
			if (t1 == 0 && t2 == 0) || (t1 != 0 && t2 != 0) {
				opstack[sp] = 0
				sp++
			} else {
				opstack[sp] = 1
				sp++
			}

		case il.And:
			if sp < 2 {
				goto STACK_UNDERFLOW
			}
			t1 = opstack[sp-1]
			t2 = opstack[sp-2]
			sp = sp - 2
			if t1 != 0 && t2 != 0 {
				opstack[sp] = 1
				sp++
			} else {
				opstack[sp] = 0
				sp++
			}

		case il.Or:
			if sp < 2 {
				goto STACK_UNDERFLOW
			}
			t1 = opstack[sp-1]
			t2 = opstack[sp-2]
			sp = sp - 2
			if t1 == 0 && t2 == 0 {
				opstack[sp] = 0
				sp++
			} else {
				opstack[sp] = 1
				sp++
			}

		case il.AXor:
			t1 = body[ip]
			ip++
			if sp < 1 {
				goto STACK_UNDERFLOW
			}
			sp--
			t2 = opstack[sp]
			if (t1 == 0 && t2 == 0) || (t1 != 0 && t2 != 0) {
				opstack[sp] = 0
				sp++
			} else {
				opstack[sp] = 1
				sp++
			}

		case il.AAnd:
			t1 = body[ip]
			ip++
			if sp < 1 {
				goto STACK_UNDERFLOW
			}
			sp--
			t2 = opstack[sp]
			if t1 != 0 && t2 != 0 {
				opstack[sp] = 1
				sp++
			} else {
				opstack[sp] = 0
				sp++
			}

		case il.AOr:
			t1 = body[ip]
			ip++
			if sp < 1 {
				goto STACK_UNDERFLOW
			}
			sp--
			t2 = opstack[sp]
			if t1 == 0 && t2 == 0 {
				opstack[sp] = 0
				sp++
			} else {
				opstack[sp] = 1
				sp++
			}

		case il.Not:
			if sp < 1 {
				goto STACK_UNDERFLOW
			}
			if opstack[sp-1] == 0 {
				opstack[sp-1] = 1
			} else {
				opstack[sp-1] = 0
			}

		case il.ResolveS:
			if sp > opStackSize-1 {
				goto STACK_OVERFLOW
			}
			t1 = body[ip]
			ip++
			tStr = strings.GetString(t1)
			tVal, tFound = bag.Get(tStr)
			if !tFound {
				tErr = fmt.Errorf("lookup failed: '%v'", tStr)
				goto RETURN_ERR
			}
			tStr, tFound = tVal.(string)
			if !tFound {
				tErr = fmt.Errorf("error converting value to string: '%v'", tVal)
				goto RETURN_ERR
			}
			opstack[sp] = strings.GetID(tStr)
			sp++

		case il.ResolveB:
			if sp > opStackSize-1 {
				goto STACK_OVERFLOW
			}
			t1 = body[ip]
			ip++
			tStr = strings.GetString(t1)
			tVal, tFound = bag.Get(tStr)
			if !tFound {
				tErr = fmt.Errorf("lookup failed: '%v'", tStr)
				goto RETURN_ERR
			}
			tBool, tFound = tVal.(bool)
			if !tFound {
				tErr = fmt.Errorf("error converting value to bool: '%v'", tVal)
				goto RETURN_ERR
			}
			if tBool {
				opstack[sp] = 1
				sp++
			} else {
				opstack[sp] = 0
				sp++
			}

		case il.ResolveI:
			if sp > opStackSize-2 {
				goto STACK_OVERFLOW
			}
			t1 = body[ip]
			ip++
			tStr = strings.GetString(t1)
			tVal, tFound = bag.Get(tStr)
			if !tFound {
				tErr = fmt.Errorf("lookup failed: '%v'", tStr)
				goto RETURN_ERR
			}
			ti64, tFound = tVal.(int64)
			if !tFound {
				tDur, tFound = tVal.(time.Duration)
				if !tFound {
					tErr = fmt.Errorf("error converting value to integer or duration: '%v'", tVal)
					goto RETURN_ERR
				}
				ti64 = int64(tDur)
			}
			opstack[sp] = uint32(ti64 >> 32)
			opstack[sp+1] = uint32(ti64 & 0xFFFFFFFF)
			sp = sp + 2

		case il.ResolveD:
			if sp > opStackSize-2 {
				goto STACK_OVERFLOW
			}
			t1 = body[ip]
			ip++
			tStr = strings.GetString(t1)
			tVal, tFound = bag.Get(tStr)
			if !tFound {
				tErr = fmt.Errorf("lookup failed: '%v'", tStr)
				goto RETURN_ERR
			}
			tf64, tFound = tVal.(float64)
			if !tFound {
				tErr = fmt.Errorf("error converting value to double: '%v'", tVal)
				goto RETURN_ERR
			}

			tu64 = math.Float64bits(tf64)
			opstack[sp] = uint32(tu64 >> 32)
			opstack[sp+1] = uint32(tu64 & 0xFFFFFFFF)
			sp = sp + 2

		case il.ResolveF:
			if sp > opStackSize-2 {
				goto STACK_OVERFLOW
			}
			t1 = body[ip]
			ip++
			tStr = strings.GetString(t1)
			tVal, tFound = bag.Get(tStr)
			if !tFound {
				tErr = fmt.Errorf("lookup failed: '%v'", tStr)
				goto RETURN_ERR
			}
			if hp == heapSize-1 {
				goto HEAP_OVERFLOW
			}
			t2 = hp
			heap[hp] = tVal
			hp++
			opstack[sp] = t2
			sp++

		case il.TResolveS:
			if sp > opStackSize-2 {
				goto STACK_OVERFLOW
			}
			t1 = body[ip]
			ip++
			tStr = strings.GetString(t1)
			tVal, tFound = bag.Get(tStr)
			if !tFound {
				opstack[sp] = 0
				sp++
			} else {
				tStr, tFound = tVal.(string)
				if !tFound {
					tErr = fmt.Errorf("error converting value to string: '%v'", tVal)
					goto RETURN_ERR
				}
				opstack[sp] = strings.GetID(tStr)
				sp++
				opstack[sp] = 1
				sp++
			}

		case il.TResolveB:
			if sp > opStackSize-2 {
				goto STACK_OVERFLOW
			}
			t1 = body[ip]
			ip++
			tStr = strings.GetString(t1)
			tVal, tFound = bag.Get(tStr)
			if !tFound {
				opstack[sp] = 0
				sp++
			} else {
				tBool, tFound = tVal.(bool)
				if !tFound {
					tErr = fmt.Errorf("error converting value to bool: '%v'", tVal)
					goto RETURN_ERR
				}
				if tBool {
					opstack[sp] = 1
					sp++
				} else {
					opstack[sp] = 0
					sp++
				}
				opstack[sp] = 1
				sp++
			}

		case il.TResolveI:
			if sp > opStackSize-3 {
				goto STACK_OVERFLOW
			}
			t1 = body[ip]
			ip++
			tStr = strings.GetString(t1)
			tVal, tFound = bag.Get(tStr)
			if !tFound {
				opstack[sp] = 0
				sp++
			} else {
				ti64, tFound = tVal.(int64)
				if !tFound {
					tDur, tFound = tVal.(time.Duration)
					if !tFound {
						tErr = fmt.Errorf("error converting value to integer or duration: '%v'", tVal)
						goto RETURN_ERR
					}
					ti64 = int64(tDur)
				}
				opstack[sp] = uint32(ti64 >> 32)
				opstack[sp+1] = uint32(ti64 & 0xFFFFFFFF)
				sp = sp + 2
				opstack[sp] = 1
				sp++
			}

		case il.TResolveD:
			if sp > opStackSize-3 {
				goto STACK_OVERFLOW
			}
			t1 = body[ip]
			ip++
			tStr = strings.GetString(t1)
			tVal, tFound = bag.Get(tStr)
			if !tFound {
				opstack[sp] = 0
				sp++
			} else {
				tf64, tFound = tVal.(float64)
				if !tFound {
					tErr = fmt.Errorf("error converting value to double: '%v'", tVal)
					goto RETURN_ERR
				}

				tu64 = math.Float64bits(tf64)
				opstack[sp] = uint32(tu64 >> 32)
				opstack[sp+1] = uint32(tu64 & 0xFFFFFFFF)
				sp = sp + 2
				opstack[sp] = 1
				sp++
			}

		case il.TResolveF:
			if sp > opStackSize-2 {
				goto STACK_OVERFLOW
			}
			t1 = body[ip]
			ip++
			tStr = strings.GetString(t1)
			tVal, tFound = bag.Get(tStr)
			if !tFound {
				opstack[sp] = 0
				sp++
			} else {
				if hp == heapSize-1 {
					goto HEAP_OVERFLOW
				}
				t2 = hp
				heap[hp] = tVal
				hp++
				opstack[sp] = t2
				sp++
				opstack[sp] = 1
				sp++
			}

		case il.AddI:
			if sp < 4 {
				goto STACK_UNDERFLOW
			}
			t1 = opstack[sp-1]
			t2 = opstack[sp-2]
			sp = sp - 2
			ti64 = int64(t1) + int64(t2)<<32
			t1 = opstack[sp-1]
			t2 = opstack[sp-2]
			sp = sp - 2
			ti64 += int64(t1) + int64(t2)<<32
			opstack[sp] = uint32(ti64 >> 32)
			opstack[sp+1] = uint32(ti64 & 0xFFFFFFFF)
			sp = sp + 2

		case il.SubI:
			if sp < 4 {
				goto STACK_UNDERFLOW
			}
			t1 = opstack[sp-1]
			t2 = opstack[sp-2]
			sp = sp - 2
			ti64 = int64(t1) + int64(t2)<<32
			ti64 *= -1
			t1 = opstack[sp-1]
			t2 = opstack[sp-2]
			sp = sp - 2
			ti64 += int64(t1) + int64(t2)<<32
			opstack[sp] = uint32(ti64 >> 32)
			opstack[sp+1] = uint32(ti64 & 0xFFFFFFFF)
			sp = sp + 2

		case il.AAddI:
			if sp < 2 {
				goto STACK_UNDERFLOW
			}
			t1 = body[ip]
			t2 = body[ip+1]
			ip = ip + 2
			ti64 = int64(t1) + int64(t2)<<32
			t1 = opstack[sp-1]
			t2 = opstack[sp-2]
			sp = sp - 2
			ti64 += int64(t1) + int64(t2)<<32
			opstack[sp] = uint32(ti64 >> 32)
			opstack[sp+1] = uint32(ti64 & 0xFFFFFFFF)
			sp = sp + 2

		case il.ASubI:
			if sp < 2 {
				goto STACK_UNDERFLOW
			}
			t1 = body[ip]
			t2 = body[ip+1]
			ip = ip + 2
			ti64 = int64(t1) + int64(t2)<<32
			ti64 *= -1
			t1 = opstack[sp-1]
			t2 = opstack[sp-2]
			sp = sp - 2
			ti64 += int64(t1) + int64(t2)<<32
			opstack[sp] = uint32(ti64 >> 32)
			opstack[sp+1] = uint32(ti64 & 0xFFFFFFFF)
			sp = sp + 2

		case il.AddD:
			if sp < 4 {
				goto STACK_UNDERFLOW
			}
			t1 = opstack[sp-1]
			t2 = opstack[sp-2]
			sp = sp - 2
			tf64 = math.Float64frombits(uint64(t1) + uint64(t2)<<32)
			t1 = opstack[sp-1]
			t2 = opstack[sp-2]
			sp = sp - 2
			tf64 += math.Float64frombits(uint64(t1) + uint64(t2)<<32)
			tu64 = math.Float64bits(tf64)
			opstack[sp] = uint32(tu64 >> 32)
			opstack[sp+1] = uint32(tu64 & 0xFFFFFFFF)
			sp = sp + 2

		case il.SubD:
			if sp < 4 {
				goto STACK_UNDERFLOW
			}
			t1 = opstack[sp-1]
			t2 = opstack[sp-2]
			sp = sp - 2
			tf64 = math.Float64frombits(uint64(t1) + uint64(t2)<<32)
			tf64 *= -1
			t1 = opstack[sp-1]
			t2 = opstack[sp-2]
			sp = sp - 2
			tf64 += math.Float64frombits(uint64(t1) + uint64(t2)<<32)
			tu64 = math.Float64bits(tf64)
			opstack[sp] = uint32(tu64 >> 32)
			opstack[sp+1] = uint32(tu64 & 0xFFFFFFFF)
			sp = sp + 2

		case il.AAddD:
			if sp < 2 {
				goto STACK_UNDERFLOW
			}
			t1 = body[ip]
			t2 = body[ip+1]
			ip = ip + 2
			tf64 = math.Float64frombits(uint64(t1) + uint64(t2)<<32)
			t1 = opstack[sp-1]
			t2 = opstack[sp-2]
			sp = sp - 2
			tf64 += math.Float64frombits(uint64(t1) + uint64(t2)<<32)
			tu64 = math.Float64bits(tf64)
			opstack[sp] = uint32(tu64 >> 32)
			opstack[sp+1] = uint32(tu64 & 0xFFFFFFFF)
			sp = sp + 2

		case il.ASubD:
			if sp < 2 {
				goto STACK_UNDERFLOW
			}
			t1 = body[ip]
			t2 = body[ip+1]
			ip = ip + 2
			tf64 = math.Float64frombits(uint64(t1) + uint64(t2)<<32)
			tf64 *= -1
			t1 = opstack[sp-1]
			t2 = opstack[sp-2]
			sp = sp - 2
			tf64 += math.Float64frombits(uint64(t1) + uint64(t2)<<32)
			tu64 = math.Float64bits(tf64)
			opstack[sp] = uint32(tu64 >> 32)
			opstack[sp+1] = uint32(tu64 & 0xFFFFFFFF)
			sp = sp + 2

		case il.Jmp:
			t1 = body[ip]
			ip++
			ip = t1

		case il.Jz:
			if sp < 1 {
				goto STACK_UNDERFLOW
			}
			t1 = body[ip]
			ip++
			sp--
			t2 = opstack[sp]
			if t2 == 0 {
				ip = t1
			}

		case il.Jnz:
			if sp < 1 {
				goto STACK_UNDERFLOW
			}
			t1 = body[ip]
			ip++
			sp--
			t2 = opstack[sp]
			if t2 != 0 {
				ip = t1
			}

		case il.Call:
			t1 = body[ip]
			ip++
			frames[fp].save(&registers, sp-typesStackAllocSize(fn.Parameters), ip, fn)
			fp++
			fn := in.program.Functions.GetByID(t1)

			if fn == nil {
				tErr = fmt.Errorf("function not found: '%s'", strings.GetString(t1))
				goto RETURN_ERR
			}
			if fn.Address == 0 {
				fp--

				ext := in.externs[strings.GetString(t1)]
				t2 = typesStackAllocSize(fn.Parameters)
				if sp < t2 {
					goto STACK_UNDERFLOW
				}
				t1, t3, tErr = ext.invoke(strings, heap, &hp, opstack, sp)
				if tErr != nil {
					goto RETURN_ERR
				}

				opstack[sp-t2] = t1
				opstack[sp-t2+1] = t3
				sp -= t2 - typeStackAllocSize(fn.ReturnType)
				break
			}

			ip = fn.Address

		case il.Ret:
			if fp == 0 {

				r := Result{
					t: fn.ReturnType,
				}
				switch fn.ReturnType {
				case il.Void, il.Integer, il.Double, il.Bool, il.Duration:

					switch typeStackAllocSize(fn.ReturnType) {
					case 0:

					case 1:
						if sp < 1 {
							goto STACK_UNDERFLOW
						}
						r.v1 = opstack[sp-1]

					case 2:
						if sp < 2 {
							goto STACK_UNDERFLOW
						}
						r.v1 = opstack[sp-1]
						r.v2 = opstack[sp-2]

					default:
						panic("interpreter.run: unhandled parameter size")
					}

				case il.String:
					if sp < 1 {
						goto STACK_UNDERFLOW
					}
					r.v1 = opstack[sp-1]
					r.vs = strings.GetString(r.v1)

				case il.Interface:
					if sp < 1 {
						goto STACK_UNDERFLOW
					}
					r.v1 = opstack[sp-1]
					r.vi = heap[r.v1]

				default:
					panic("interpreter.run: unhandled return type")
				}

				if step {
					copy(in.stepper.registers[:], registers[:])
					in.stepper.sp = sp
					in.stepper.ip = ip
					in.stepper.fp = fp
					copy(in.stepper.opstack, opstack)
					copy(in.stepper.frames, frames)
					copy(in.stepper.heap, heap)
					in.stepper.hp = hp
					in.stepper.completed = true
				}

				return r, nil
			}

			t1 = typeStackAllocSize(fn.ReturnType)
			t2 = sp
			fp--
			frames[fp].restore(&registers, &sp, &ip, &fn)
			for t3 = 0; t3 < t1; t3++ {
				opstack[sp+t3] = opstack[t2-t1+t3]
			}
			sp += t1

		case il.TLookup:
			if sp < 2 {
				goto STACK_UNDERFLOW
			}
			t1 = opstack[sp-1]
			t2 = opstack[sp-2]
			sp = sp - 2
			tStr = strings.GetString(t1)
			if t2 >= hp {
				goto INVALID_HEAP_ACCESS
			}
			tVal = heap[t2]
			tStr, tFound = tVal.(map[string]string)[tStr]
			if tFound {
				t3 = strings.GetID(tStr)
				opstack[sp] = t3
				opstack[sp+1] = 1
				sp = sp + 2
			} else {
				opstack[sp] = 0
				sp++
			}

		case il.Lookup:
			if sp < 2 {
				goto STACK_UNDERFLOW
			}
			t1 = opstack[sp-1]
			t2 = opstack[sp-2]
			sp = sp - 2
			tStr = strings.GetString(t1)
			if t2 >= hp {
				goto INVALID_HEAP_ACCESS
			}
			tVal = heap[t2]
			tStr, tFound = tVal.(map[string]string)[tStr]
			if !tFound {
				tErr = fmt.Errorf("member lookup failed: '%v'", strings.GetString(t1))
				goto RETURN_ERR
			}
			t3 = strings.GetID(tStr)
			opstack[sp] = t3
			sp++

		case il.NLookup:
			if sp < 2 {
				goto STACK_UNDERFLOW
			}
			t1 = opstack[sp-1]
			t2 = opstack[sp-2]
			sp = sp - 2
			tStr = strings.GetString(t1)
			if t2 >= hp {
				goto INVALID_HEAP_ACCESS
			}
			tVal = heap[t2]
			tStr, tFound = tVal.(map[string]string)[tStr]
			if !tFound {
				tStr = ""
			}
			t3 = strings.GetID(tStr)
			opstack[sp] = t3
			sp++

		case il.ALookup:
			if sp < 1 {
				goto STACK_UNDERFLOW
			}
			t1 = body[ip]
			ip++
			tStr = strings.GetString(t1)
			sp--
			t2 = opstack[sp]
			if t2 >= hp {
				goto INVALID_HEAP_ACCESS
			}
			tVal = heap[t2]
			tStr, tFound = tVal.(map[string]string)[tStr]
			if !tFound {
				tErr = fmt.Errorf("member lookup failed: '%v'", strings.GetString(t1))
				goto RETURN_ERR
			}
			t3 = strings.GetID(tStr)
			opstack[sp] = t3
			sp++

		case il.ANLookup:
			if sp < 1 {
				goto STACK_UNDERFLOW
			}
			t1 = body[ip]
			ip++
			tStr = strings.GetString(t1)
			sp--
			t2 = opstack[sp]
			if t2 >= hp {
				goto INVALID_HEAP_ACCESS
			}
			tVal = heap[t2]
			tStr, tFound = tVal.(map[string]string)[tStr]
			if !tFound {
				tStr = ""
			}
			t3 = strings.GetID(tStr)
			opstack[sp] = t3
			sp++

		default:
			tErr = fmt.Errorf("invalid opcode: '%v'", il.Opcode(code))
			goto RETURN_ERR
		}

		if step {
			copy(in.stepper.registers[:], registers[:])
			in.stepper.sp = sp
			in.stepper.ip = ip
			in.stepper.fp = fp
			copy(in.stepper.opstack, opstack)
			copy(in.stepper.frames, frames)
			copy(in.stepper.heap, heap)
			in.stepper.hp = hp

			return Result{}, nil
		}
	}

STACK_OVERFLOW:
	tErr = errors.New("stack overflow")
	goto RETURN_ERR
STACK_UNDERFLOW:
	tErr = errors.New("stack underflow")
	goto RETURN_ERR
INVALID_HEAP_ACCESS:
	tErr = errors.New("invalid heap access")
	goto RETURN_ERR
HEAP_OVERFLOW:
	tErr = errors.New("heap overflow")
	goto RETURN_ERR

RETURN_ERR:
	if step {
		in.stepper.completed = true
	}
	return Result{}, tErr
}
