#compdef mk

_mk() {
    local -a targets flags

    flags=(
        '-C[change to directory before doing anything]:dir:_directories'
        '-f[mkfile to read]:file:_files'
        '-v[verbose output]'
        '-B[unconditional rebuild]'
        '-n[dry run]'
        '-j[parallel jobs]:jobs:'
        '--why[explain why targets are stale]'
        '--graph[print dependency subgraph]'
        '--state[show build database entries]'
        '--help-agent[print the mk agents guide]'
        '--version[print version and exit]'
    )

    # Get targets and configs from mkfile
    targets=(${(f)"$(mk --complete 2>/dev/null)"})

    _arguments -s $flags '*:target:compadd -a targets'
}

_mk "$@"
