_mk() {
    local cur="${COMP_WORDS[COMP_CWORD]}"

    # Complete flags
    if [[ "$cur" == -* ]]; then
        COMPREPLY=($(compgen -W "-f -v -B -n -j --why --graph --state" -- "$cur"))
        return
    fi

    # Complete targets and configs from mkfile
    local targets
    targets=$(mk --complete 2>/dev/null)
    COMPREPLY=($(compgen -W "$targets" -- "$cur"))
}

complete -F _mk mk
