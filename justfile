# Recipes for building and installing budget. Run `just` to list them.

bin := "budget"
# Where `just install` puts the binary; ~/.local/bin unless BUDGET_INSTALL_DIR says otherwise.
install_dir := env("BUDGET_INSTALL_DIR", home_directory() / ".local" / "bin")
# The latest tag for a build from here, with -dirty for uncommitted changes.
version := `git describe --tags --always --dirty 2>/dev/null || echo dev`

# List the recipes
default:
    @just --list --unsorted

# Build ./budget
build:
    go build -ldflags "-s -w -X main.version={{ version }}" -o {{ bin }} .

# Run the app from source
run:
    go run -ldflags "-X main.version={{ version }}" .

# Run the tests
test:
    go test ./...

# Check formatting, vet and test
check:
    #!/usr/bin/env bash
    set -euo pipefail
    unformatted=$(gofmt -l .)
    if [ -n "$unformatted" ]; then
        echo "Not gofmt-ed:"; echo "$unformatted"; exit 1
    fi
    go vet ./...
    go test ./...

# Format the code
fmt:
    gofmt -w .

# Print the version a build from here gets
version:
    @echo {{ version }}

# Remove ./budget
clean:
    rm -f {{ bin }}

# Tag a new version (bump: patch, minor or major) and install it into install_dir
install bump="patch":
    #!/usr/bin/env bash
    set -euo pipefail
    yellow=$'\033[33m'; green=$'\033[32m'; off=$'\033[0m'
    ask() { read -r -p "$1 [y/N] " ok; [[ "$ok" =~ ^[yY]$ ]] || { echo "Cancelled, nothing installed."; exit 1; }; }

    case "{{ bump }}" in patch|minor|major) ;; *) echo "bump must be patch, minor or major, not {{ bump }}"; exit 1 ;; esac

    branch=$(git branch --show-current)
    if [ "$branch" != "main" ]; then
        echo "${yellow}Warning: you are on ${branch:-a detached HEAD}, not main.${off}"
        ask "Tag and install a version from here anyway?"
    fi
    dirty=""
    if [ -n "$(git status --porcelain)" ]; then
        echo "${yellow}Warning: there are uncommitted changes. They go into the binary but not the tag.${off}"
        ask "Install anyway?"
        dirty="-dirty"
    fi

    # Reinstall when HEAD already carries a version, otherwise bump the highest one.
    if current=$(git describe --tags --exact-match --match 'v[0-9]*' HEAD 2>/dev/null); then
        next=$current
        echo "HEAD is already $next; reinstalling it."
    else
        last=$(git tag --list 'v[0-9]*.[0-9]*.[0-9]*' --sort=-v:refname | head -n1)
        if [ -z "$last" ]; then
            next=v0.1.0
        else
            IFS=. read -r major minor patch <<< "${last#v}"
            patch=${patch%%[-+]*}
            case "{{ bump }}" in
                major) next="v$((major + 1)).0.0" ;;
                minor) next="v$major.$((minor + 1)).0" ;;
                patch) next="v$major.$minor.$((patch + 1))" ;;
            esac
        fi
        echo "Next version: $next (last was ${last:-none})"
    fi

    go test ./...
    mkdir -p "{{ install_dir }}"
    tmp="{{ install_dir }}/.{{ bin }}.tmp"
    trap 'rm -f "$tmp"' EXIT
    go build -ldflags "-s -w -X main.version=$next$dirty" -o "$tmp" .
    if ! git rev-parse -q --verify "refs/tags/$next" >/dev/null; then
        git tag -a "$next" -m "budget $next"
        echo "Tagged $next (not pushed; git push origin $next to share it)."
    fi
    mv -f "$tmp" "{{ install_dir }}/{{ bin }}"
    echo "${green}Installed budget $next$dirty to {{ install_dir }}/{{ bin }}${off}"

    case ":$PATH:" in *":{{ install_dir }}:"*) ;; *) echo "${yellow}{{ install_dir }} is not on your PATH.${off}" ;; esac
    found=$(command -v {{ bin }} || true)
    if [ -n "$found" ] && [ "$found" != "{{ install_dir }}/{{ bin }}" ]; then
        echo "${yellow}Note: '{{ bin }}' runs $found first on your PATH.${off}"
    fi
