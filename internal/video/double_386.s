//go:build windows

#include "textflag.h"

TEXT ·doubleThunk(SB),NOSPLIT,$0-12
	MOVL	fn+0(FP), CX
	MOVL	this+4(FP), AX
	SUBL	$4, SP
	MOVL	AX, 0(SP) // fn pops it (stdcall)
	CALL	CX
	MOVL	out+8(FP), CX
	FMOVDP	F0, 0(CX) // pops ST0, leaving the x87 stack empty for Go
	RET               // syscall puts SP back itself

TEXT ·doubleThunkAddr(SB),NOSPLIT,$0-4
	MOVL	$·doubleThunk(SB), AX
	MOVL	AX, ret+0(FP)
	RET
