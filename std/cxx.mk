cxx ?= c++
cxxflags ?= -Wall
ldflags ?=

{name}.o: {name}.cc
    $cxx $cxxflags -c $input -o $target
