cc ?= cc
cflags ?= -Wall
ldflags ?=
ar ?= ar

{name}.o: {name}.c
    $cc $cflags -c $input -o $target
