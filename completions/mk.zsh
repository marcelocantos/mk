#compdef mk

_mk() {
    local -a targets flags

    flags=(
        '-f[mkfile to read]:file:_files'
        '-v[verbose output]'
        '-B[unconditional rebuild]'
        '-n[dry run]'
        '-j[parallel jobs]:jobs:'
        '--why[explain why targets are stale]'
        '--graph[print dependency subgraph]'
        '--state[show build database entries]'
        '--agents-guide[print the mk agents guide]'
    )

    # Get targets and configs from mkfile
    targets=(${(f)"$(mk --complete 2>/dev/null)"})

    _arguments -s $flags '*:target:compadd -a targets'
}

_mk "$@"
