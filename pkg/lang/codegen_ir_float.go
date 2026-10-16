// codegen_ir_float.go holds the runtime block that renders a double: floatRuntimeIR, the one text the
// float roads call when the value they must print is not a constant the compiler already read.
package lang

// floatRuntimeIR is the double-rendering block: the doors that turn a runtime double into the text
// str() would have printed. It is a runtime block rather than a fold because the value is only known
// once the program has run — where the constant's own bits are decided by floatConst in codegen_float.go.
const floatRuntimeIR = `; rt_fmt_double renders a runtime double the way Python's str() does, because the
; printf formats alone cannot: %g loses precision (0.123456789 -> 0.123457) and
; %.17g invents digits (0.1 -> 0.10000000000000001), and neither prints the
; trailing ".0" that marks an integral float (print(x * 2.0) showed 2). It tries
; the shortest precision that round-trips through strtod, then appends ".0" when
; the text has no decimal point or exponent (and is not inf/nan). The result
; lives in a small rotating buffer set so one print statement can format several
; values before handing them to printf.
@rt.fd.bufs = private global [8 x [64 x i8]] zeroinitializer
@rt.fd.next = private global i32 0
@rt.fd.fmt = private constant [5 x i8] c"%.*g\00"

declare double @strtod(i8*, i8**)

define internal i8* @rt_fmt_double(double %v) {
entry:
  %slot = load i32, i32* @rt.fd.next
  %ns = add i32 %slot, 1
  %nw = urem i32 %ns, 8
  store i32 %nw, i32* @rt.fd.next
  %buf = getelementptr [8 x [64 x i8]], [8 x [64 x i8]]* @rt.fd.bufs, i32 0, i32 %slot, i32 0
  br label %try
try:
  %prec = phi i32 [ 15, %entry ], [ %pn, %bad ]
  %fmtp = getelementptr inbounds [5 x i8], [5 x i8]* @rt.fd.fmt, i32 0, i32 0
  %w = call i32 (i8*, i32, i8*, ...) @snprintf(i8* %buf, i32 64, i8* %fmtp, i32 %prec, double %v)
  %back = call double @strtod(i8* %buf, i8* null)
  %same = fcmp oeq double %back, %v
  br i1 %same, label %fix, label %bad
bad:
  %pn = add i32 %prec, 1
  %more = icmp slt i32 %prec, 17
  br i1 %more, label %try, label %fix
fix:
  ; snprintf told us how many characters it wrote; that is where the ".0" goes.
  %len = phi i32 [ %w, %try ], [ %w, %bad ]
  %clamped = icmp ult i32 %len, 62
  %safe = select i1 %clamped, i32 %len, i32 62
  ; Decide the trailing ".0" from the value, not from the text: appending it is
  ; correct exactly when the number is a finite integral value small enough that
  ; %g rendered it positionally (Python: 2.0 -> "2.0", but 1e+16 -> "1e+16" and
  ; 3.5 -> "3.5" take no suffix).
  %fl = call double @llvm.floor.f64(double %v)
  %integral = fcmp oeq double %fl, %v
  %an = call double @llvm.fabs.f64(double %v)
  %finite = fcmp olt double %an, 1.000000e+308
  %positional = fcmp olt double %an, 1.000000e+15
  %c1 = and i1 %integral, %finite
  %needs = and i1 %c1, %positional
  br i1 %needs, label %addzero, label %done
addzero:
  %p0 = getelementptr i8, i8* %buf, i32 %safe
  store i8 46, i8* %p0
  %i1 = add i32 %safe, 1
  %p1 = getelementptr i8, i8* %buf, i32 %i1
  store i8 48, i8* %p1
  %i2 = add i32 %safe, 2
  %p2 = getelementptr i8, i8* %buf, i32 %i2
  store i8 0, i8* %p2
  br label %done
done:
  ret i8* %buf
}

; rt_round_digits answers round(x, ndigits) by moving the decimal point, which is the one
; rounding question the binary domain cannot answer — not because ties are subtle, but because
; scaling manufactures ties the value never had. round(0.005, 2) is 0.01 in CPython (the exact
; value of that double is 0.00500000000000000010408…, above the tie), while multiplying by 100
; gives exactly 0.5, which roundeven answers 0 (roadmap Gap R.69, ADR 0263). The same family:
; round(0.025, 2) is 0.03 and scales to 2.5, round(0.075, 2) is 0.07 and scales to 7.5,
; round(2.675, 2) is 2.67 and scales to 267.5. What CPython rounds is the *exact decimal value of
; the double*, ties to even, and the answer is the nearest double to that decimal. That is a
; correctly-rounded double-to-decimal conversion — the same named operation the record asks
; of Go's dtoa (strconv.FormatFloat/ParseFloat), asked here of the C library's. The two were swept
; against CPython over 531,272 (value, ndigits) pairs compared bit for bit and agree with it
; everywhere but the 143 far-magnitude negative-digit cases their own scale step causes (all
; between |x| = 4.117e18 and 1e300, all one ULP).
;
; The two clamps are facts, not moods: every double is exactly a decimal with at most 324 digits
; after the point (the smallest subnormal is 4.94e-324), so rounding at 324 places or beyond is
; the identity and the text stays inside the buffer; and 10^309 is past the largest finite
; double, so the nearest multiple of it to any finite value is zero with the sign kept — which is
; what CPython answers there as well. %.0f of the scaled quotient is the digit-free form of the
; same conversion, and multiplying back lands on the answer for the negative digit counts.
@rt.rd.buf = private global [768 x i8] zeroinitializer
@rt.rd.fmt = private constant [5 x i8] c"%.*f\00"

define internal double @rt_round_digits(double %v, i32 %n) {
entry:
  %wide = icmp sge i32 %n, 324
  %deep = icmp slt i32 %n, -308
  %nsc = select i1 %deep, i32 -308, i32 %n
  %nuse = select i1 %wide, i32 0, i32 %nsc
  %down = icmp slt i32 %nuse, 0
  ; The sign of the zero the deep branch answers with, taken from the value's own bits so that
  ; round(-0.0, -400) is -0.0 and not 0.0 — a fcmp cannot tell those two apart.
  %bits = bitcast double %v to i64
  %sign = and i64 %bits, -9223372036854775808
  %negz = icmp ne i64 %sign, 0
  %zero = select i1 %negz, double -0.000000e+00, double 0.000000e+00
  br i1 %wide, label %asis, label %deepcheck
asis:
  ; Beyond every double's decimal expansion: the value it arrived with.
  br label %done
deepcheck:
  br i1 %deep, label %zeroed, label %pick
zeroed:
  br label %done
pick:
  br i1 %down, label %scaled, label %text
scaled:
  ; 10^(-n) one multiplication at a time — the sequence the record's pow10 walks, so the
  ; two backends round the same products rather than each finding its own power.
  br label %sloop
sloop:
  %s = phi double [ 1.000000e+00, %scaled ], [ %sm, %sbody ]
  %i = phi i32 [ 0, %scaled ], [ %inext, %sbody ]
  %m = sub i32 0, %nuse
  %more = icmp slt i32 %i, %m
  br i1 %more, label %sbody, label %sdone
sbody:
  %sm = fmul double %s, 1.000000e+01
  %inext = add i32 %i, 1
  br label %sloop
sdone:
  %q = fdiv double %v, %s
  br label %text
text:
  ; One conversion, asked two ways: at %nuse places for a digit count that reaches into the
  ; fraction, at none at all for a digit count that reaches past the point.
  %tv = phi double [ %v, %pick ], [ %q, %sdone ]
  %tp = phi i32 [ %nuse, %pick ], [ 0, %sdone ]
  %tm = phi double [ 1.000000e+00, %pick ], [ %s, %sdone ]
  %bp = getelementptr [768 x i8], [768 x i8]* @rt.rd.buf, i32 0, i32 0
  %fp = getelementptr [5 x i8], [5 x i8]* @rt.rd.fmt, i32 0, i32 0
  %w = call i32 (i8*, i32, i8*, ...) @snprintf(i8* %bp, i32 768, i8* %fp, i32 %tp, double %tv)
  %back = call double @strtod(i8* %bp, i8* null)
  %res = fmul double %back, %tm
  br label %done
done:
  %out = phi double [ %v, %asis ], [ %zero, %zeroed ], [ %res, %text ]
  ret double %out
}

`
