cc ?= cc
cflags ?= -Wall
ldflags ?=
ar ?= ar
ccache ?= $[shell command -v ccache 2>/dev/null]

{name}.o: {name}.c
    $ccache $cc $cflags -c $input -o $target
