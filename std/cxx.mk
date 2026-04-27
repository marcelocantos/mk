cxx ?= c++
cxxflags ?= -Wall
ldflags ?=
ccache ?= $[shell command -v ccache 2>/dev/null]

{name}.o: {name}.cc
    $ccache $cxx $cxxflags -c $input -o $target
