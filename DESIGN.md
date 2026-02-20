# mk Design Spec

A build tool with Make's dependency-graph model, minus 48 years of
accumulated pain.

## Philosophy

Same execution model as Make: a declared dependency DAG, recipes that
produce targets from prerequisites, parallel execution, only stale
targets rebuilt. What changes: content hashing, sane defaults, clean
syntax, first-class support for things Make bolted on after the fact.

mk is not a radical reimagination. It is Make with the mistakes fixed.

---

## 1. Variables

### Assignment

```
cc = gcc                        # immediate (always)
cflags = -Wall -O2              # immediate
cflags += -Werror               # append
lazy version = $(shell git describe)   # explicit deferred evaluation
```

All assignments are immediate by default. `lazy` defers evaluation
until first use. Recursive definitions (`foo = $foo bar`) are a
parse error.

### Reference

`$name` references a variable. Multi-character names work without
delimiters — there is no single-character parse rule. `$foo` means
the variable `foo`, not `$(f)` followed by `oo`.

`${name}` delimits when the variable is adjacent to identifier
characters: `${foo}bar`.

`$$` produces a literal `$` (for shell variables in recipes).

### Substitution references

```
obj = $src:.c=.o
```

Replaces the suffix `.c` with `.o` in every word of `$src`.

### Environment

All variables are environment variables. Recipes see them without
`export`. Command-line overrides beat mkfile assignments beat
inherited environment. One rule, no flags.

```
$ mk cc=clang test        # overrides cc for this invocation
```

### Conditional assignment

```
csp_include ?= include          # set only if not already defined
```

---

## 2. Rules

```
build/{name}.o: src/{name}.c
    $cc $cflags -c $input -o $target
```

- **Indentation:** any whitespace (spaces or tabs).
- **Single shell:** the entire recipe block runs as one `sh -c`
  invocation with `set -e`. `cd` persists across lines. No `\`
  continuation needed for multi-line logic.
- **Auto-mkdir:** parent directories of targets are created
  automatically.
- **Delete on error:** if a recipe fails, the partial target is
  removed. This is Make's `.DELETE_ON_ERROR`, but default.
- **Line continuations:** a trailing `\` joins the next line, for
  readability of long variable values or prerequisite lists.

### Recipe prefixes

| Prefix | Meaning |
|--------|---------|
| `@`    | Silent — don't echo this line |
| `-`    | Ignore errors on this line |

### Automatic variables

| Name | Meaning |
|------|---------|
| `$target` | Target being built |
| `$input` | First prerequisite |
| `$inputs` | All prerequisites (space-separated) |
| `$changed` | Prerequisites that changed since last build |
| `$stem` | Matched stem (single-capture shorthand) |
| `$target.dir` | Directory part of target |
| `$target.file` | Filename part of target |

No `$@`, `$<`, `$^`. One set of names.

---

## 3. Tasks

```
!clean:
    rm -rf build/ .mk/

!test: build/app
    ./build/app --self-test

!deploy: build/app.img
    docker push myapp:latest
```

The `!` prefix declares "this is an action, not a file." Tasks always
run when requested — there is no staleness check. In prerequisite
position, tasks are referenced by name without `!`:

```
!test-dist: test test:dist
```

---

## 4. Patterns

Named captures replace Make's `%`:

```
build/{name}.o: src/{name}.c
    $cc $cflags -c $input -o $target
```

Same name on both sides means values must match. Multiple captures
are allowed:

```
build/{arch}/{config}/{name}.o: src/{name}.c
    ${cc_$arch} ${cflags_$config} -c $input -o $target
```

Captures bind when a target is requested. Requesting
`build/arm64/release/foo.o` binds `arch=arm64`, `config=release`,
`name=foo`. Capture values are available as variables in the recipe.

Captures must not contain `/` — each capture matches within a single
path segment.

### Disambiguation

When multiple pattern rules could match a target, mk selects the
rule with the most literal (non-capture) characters. Ties are an
error.

---

## 5. Multi-output rules

```
gen/{name}.pb.h gen/{name}.pb.cc: proto/{name}.proto
    protoc --cpp_out=gen/ $input
```

Multiple targets on the left of `:` means one invocation produces
all outputs. Always. No ambiguity, no special syntax.

The build database tracks all outputs together. If any output is
missing or stale, the recipe runs once. The `$target` variable
refers to the first listed target.

---

## 6. Configs

Named configurations for build variants. Configs compose.

### Declaration

```
config debug:
    cxxflags += -O0 -g -DDEBUG

config release:
    excludes debug
    cxxflags += -O2 -DNDEBUG

config asan:
    cxxflags += -fsanitize=address -fno-omit-frame-pointer
    ldflags += -fsanitize=address

config dist:
    requires dist
    csp_include = dist
```

### Properties

| Property | Meaning |
|----------|---------|
| `excludes <config>` | Mutual exclusion. `mk test:debug+release` is an error. |
| `requires <target>` | Prerequisite. Ensures the named target has been built before any `:config` builds proceed. |
| Variable assignments | Override or append to base variables. |

### Usage

```
$ mk test              # base config
$ mk test:debug        # debug config
$ mk test:debug+asan   # debug + asan composed
$ mk test:dist         # test against distribution build
```

### Composition

`:` separates target from config. `+` combines configs. Configs
stack left-to-right: `test:debug+asan` applies `debug` overrides,
then `asan` on top. `+=` accumulates; `=` from a later config
overrides an earlier one.

### Build directory

mk auto-derives the build directory by appending config names to the
base `builddir`:

```
builddir = build
# mk test:debug+asan → builddir = build-debug-asan
```

The build database tracks each config combination independently.

---

## 7. Build database

Stored in `.mk/` (like `.git/`). Tracks per target:

- **Prerequisite set.** If the set changes — additions or deletions —
  the target is stale. Delete a source file? Prerequisite set changed.
  Rebuild.
- **Recipe text** (after variable expansion). Change `-O2` to `-O0`?
  Recipe changed. Rebuild. Change a comment in the mkfile? Recipe
  unchanged. No rebuild.
- **Input fingerprints.** Content hash (SHA-256) of each prerequisite
  at last build time. Modify a file then revert? Hash matches. No
  rebuild.
- **Output fingerprint.** Detects targets modified outside the build.

### Performance

Content hashing uses an `(path, mtime, size) → hash` cache. Only
re-reads files whose metadata changed. Nearly as fast as `stat()`.

### Non-file artifacts

Annotation for custom fingerprinting:

```
app.img [fingerprint: docker inspect --format '{{.Id}}' myapp]:
        build/app Dockerfile
    docker build -t myapp .

db/schema [fingerprint: ./schema-version]:
    migrate up
```

The fingerprint command outputs a stable string. If it changes since
last build, the target is stale.

---

## 8. Conditionals

```
if $cc == gcc
    cflags += -Wextra
elif $cc == clang
    cflags += -Weverything
else
    cflags += -Wall
end
```

Comparisons: `==`, `!=`. Operands are expanded before comparison.
Conditionals can appear at file scope or inside other conditionals.

---

## 9. Functions

### Built-in functions

| Function | Description |
|----------|-------------|
| `$(wildcard pattern)` | Glob file paths |
| `$(shell command)` | Run a shell command, capture stdout |
| `$(patsubst pat,repl,text)` | Pattern substitution across words |
| `$(subst from,to,text)` | Simple string substitution |
| `$(filter pattern,text)` | Keep words matching pattern |
| `$(filter-out pattern,text)` | Remove words matching pattern |
| `$(dir paths)` | Directory part of each path |
| `$(notdir paths)` | Filename part of each path |
| `$(basename paths)` | Strip suffix from each path |
| `$(suffix paths)` | Extract suffix from each path |
| `$(addprefix prefix,list)` | Prepend to each word |
| `$(addsuffix suffix,list)` | Append to each word |
| `$(sort list)` | Sort and deduplicate |
| `$(word n,list)` | Nth word (1-indexed) |
| `$(words list)` | Word count |
| `$(strip text)` | Normalize whitespace |
| `$(if cond,then,else)` | Conditional expansion |
| `$(findstring needle,haystack)` | Search for substring |

### User-defined functions

```
fn objpath(src):
    return $src:src/%.c=build/%.o
```

Invoked as `$(objpath $src)`. Named parameters, no positional
`$(1)`/`$(2)`.

### Loops

For generating rules across a matrix:

```
configs = debug release

for config in $configs:
    cflags_$config = $cflags ${cflags_extra_$config}
```

---

## 10. Includes

```
include std/c.mk              # opt-in standard rules
include lib/build.mk as lib   # scoped: lib.obj, lib.cflags, etc.
include common.mk             # unscoped paste
```

Scoped includes prevent variable pollution. All variables and rules
from the included file live under the alias prefix.

The standard library (`std/`) provides conventional rules for common
languages:
- `std/c.mk` — C compilation (`cc`, `cflags`, pattern rules)
- `std/cxx.mk` — C++ compilation
- `std/go.mk` — Go build

These are opt-in. mk has no implicit rules and no built-in variables.

---

## 11. Parallel execution

```
$ mk -j8 test
$ mk -j0 test          # number of CPUs
```

mk builds independent targets concurrently. The dependency graph
determines ordering; siblings in the DAG run in parallel.

Parallel execution respects rule boundaries — a recipe is atomic.
Two recipes never interleave their output. Stdout and stderr from
each recipe are buffered and printed together on completion.

---

## 12. Command-line interface

```
mk [flags] [target...] [var=value...]
```

| Flag | Meaning |
|------|---------|
| `-f FILE` | Read FILE instead of `mkfile` |
| `-j N` | Parallel jobs (0 = number of CPUs) |
| `-v` | Verbose — print recipe commands |
| `-n` | Dry run — print what would be built |
| `-B` | Unconditional rebuild (ignore build database) |

Targets and variable assignments can be intermixed:

```
$ mk cc=clang test:asan -j0
```

If no target is specified, mk builds the first non-task rule.

### Diagnostic commands

| Command | Meaning |
|---------|---------|
| `mk why <target>` | Explain why a target is stale |
| `mk graph <target>` | Print the dependency subgraph |
| `mk state <target>` | Show build database entry |

---

## 13. What's removed

| Make feature | mk stance |
|---|---|
| Tab-only indentation | Any whitespace |
| `$x` as `$(x)` single-char parse | `$name` means `name` |
| `=` (recursive/lazy by default) | `=` is immediate; `lazy` keyword for deferred |
| Suffix rules (`.c.o:`) | Removed |
| Implicit rules | Removed — use `include std/c.mk` |
| Built-in variables (`CC`, `CFLAGS`) | Removed — use `include std/c.mk` |
| `.PHONY` | `!` prefix |
| `.DELETE_ON_ERROR` | Default behavior |
| `.PRECIOUS` / `.INTERMEDIATE` / `.SECONDARY` | Single `[keep]` annotation |
| `.ONESHELL` | Default behavior |
| `VPATH` / `vpath` | Removed — use explicit paths or scoped includes |
| `$(eval)` | `for` loops + `fn` |
| `define`/`endef` | `fn` |
| `$(call func,$(1),$(2))` | `$(func arg1 arg2)` with named params |
| `$(MAKE)` recursive make | Configs (`:config`), scoped includes |
| Double-colon rules | Removed |
| Archive members `lib(member)` | Removed |
| `-include *.d` dependency ritual | Build database tracks deps |
| `%` (single anonymous stem) | `{name}` (named, multiple) |
| `$$` for shell `$` in recipes | Same (`$$`) — but rarely needed since single-shell recipes reduce escaping |
| `export` / `unexport` | All variables are environment |
| `override` | Command-line always wins |
| `ifeq ($(X),val)` | `if $X == val` |
| `.RECIPEPREFIX` | Any whitespace |
| `MAKEFLAGS` | `-j` flag, not a variable |

---

## 14. What's kept

| Feature | Notes |
|---|---|
| Dependency DAG execution | Core model unchanged |
| Timestamp-free staleness | Upgraded: content hashing replaces mtime |
| Pattern rules | `{name}` replaces `%`, but same concept |
| Parallel execution (`-j`) | Same |
| `@` / `-` recipe prefixes | Same |
| `$(wildcard)`, `$(shell)`, `$(patsubst)` | Same syntax |
| `include` | Extended with `as` scoping |
| `-n` dry run | More accurate with build database |
| Command-line variable overrides | Same: `mk CC=clang` |
| Substitution references | `$var:.c=.o` |

---

## 15. Example: full project

```
# C++ project with tests, benchmarks, sanitizer support

include std/cxx.mk

cxx = c++ -std=c++17 -stdlib=libc++
cxxflags = -O2 -g -Wall -Wextra
ldflags =
ldlibs =
builddir = build

includes = -Iinclude -Ithird_party

config debug:
    excludes release
    cxxflags += -O0 -DDEBUG

config release:
    excludes debug
    cxxflags += -O2 -DNDEBUG

config asan:
    excludes tsan
    cxxflags += -fsanitize=address,undefined -fno-omit-frame-pointer
    ldflags += -fsanitize=address,undefined

config tsan:
    excludes asan
    cxxflags += -fsanitize=thread
    ldflags += -fsanitize=thread

config dist:
    requires dist
    csp_include = dist
    includes = -Ithird_party

# --- Sources ---

lib_srcs = src/csp.cc src/channel.cc src/runtime.cpp \
           src/reactor.cc src/stack_pool.cc
test_srcs = test/main.cc $(wildcard test/*.test.cc)
bench_srcs = $(wildcard bench/*.bench.cc)

lib_objs = $(patsubst %.cc,$builddir/%.o,$(patsubst %.cpp,$builddir/%.o,$lib_srcs))
test_objs = $(patsubst %.cc,$builddir/%.o,$test_srcs)
bench_objs = $(patsubst %.cc,$builddir/%.o,$bench_srcs)

# --- Rules ---

$builddir/src/{name}.o: src/{name}.cc
    $cxx $cxxflags $includes -c $input -o $target

$builddir/src/{name}.o: src/{name}.cpp
    $cxx $cxxflags $includes -c $input -o $target

$builddir/test/{name}.o: test/{name}.cc
    $cxx $cxxflags $includes -Itest -c $input -o $target

$builddir/bench/{name}.o: bench/{name}.cc
    $cxx $cxxflags $includes -c $input -o $target

$builddir/csp_tests: $lib_objs $test_objs
    $cxx $cxxflags $ldflags -o $target $inputs $ldlibs

$builddir/csp_bench: $lib_objs $bench_objs
    $cxx $cxxflags $ldflags -o $target $inputs $ldlibs

# --- Tasks ---

!test: $builddir/csp_tests
    ./$input

!bench: $builddir/csp_bench
    ./$input

!dist:
    python3 scripts/amalgamate.py

!test-dist: test test:dist

!clean:
    rm -rf build build-* dist .mk/
```

```
$ mk                     # build + run tests
$ mk test:asan           # ASan + UBSan
$ mk test:debug+asan     # debug + ASan
$ mk test:dist           # test distribution build
$ mk bench:release -j0   # release benchmarks, all cores
$ mk clean               # remove everything
$ mk why build/src/csp.o # explain why it's stale
```
