#!/bin/sh
# Holds the chart to giving the api and the worker each a metrics target the
# ServiceMonitor scrapes, and no other Service a metrics port (#451). It reads
# the golden renders, which `make helm-check` keeps equal to the chart.
set -u

here=$(cd "$(dirname "$0")" && pwd)
golden="$here/../charts/armature/tests/golden"
failed=0

fail() {
	printf 'FAIL %s\n' "$1"
	failed=1
}

# One rendered manifest, picked by its kind and name.
manifest() { # file kind name
	awk -v kind="$2" -v name="$3" '
		/^---$/ { if (k && n) printf "%s", doc; doc = ""; k = n = 0; next }
		{ doc = doc $0 "\n" }
		$0 == "kind: " kind { k = 1 }
		$0 == "  name: " name { n = 1 }
		END { if (k && n) printf "%s", doc }' "$1"
}

# The names of the Services that offer a port of the given name.
services_with_port() { # file port
	awk -v port="$2" '
		/^---$/ { if (svc && has) print name; svc = has = 0; name = ""; next }
		/^kind: Service$/ { svc = 1 }
		/^  name: / && name == "" { name = $2 }
		$0 == "    - name: " port { has = 1 }
		END { if (svc && has) print name }' "$1" | sort -u | tr '\n' ' ' | sed 's/ $//'
}

monitors=0
for file in "$golden"/*.yaml; do
	render=$(basename "$file" .yaml)
	got=$(services_with_port "$file" metrics)
	if [ "$got" != "armature-api armature-worker" ]; then
		fail "in the $render render the Services with a metrics port are \"$got\", not the api's and the worker's; only those two may offer one."
	fi
	worker=$(manifest "$file" Service armature-worker)
	[ -n "$worker" ] || fail "the $render render has no Service armature-worker; the ServiceMonitor needs one to find the worker's metrics."
	[ -z "$worker" ] || for line in '  clusterIP: None' '    - name: metrics' '      targetPort: metrics' '    app.kubernetes.io/component: worker'; do
		if ! printf '%s' "$worker" | grep -qxF -- "$line"; then
			fail "in the $render render the worker's Service lacks \"$line\"; it has to be headless and point at the worker's metrics port."
		fi
	done
	monitor=$(manifest "$file" ServiceMonitor armature)
	[ -n "$monitor" ] || continue
	monitors=$((monitors + 1))
	if ! printf '%s' "$monitor" | grep -qxF '        values: [api, worker]'; then
		fail "in the $render render the ServiceMonitor does not select both the api and the worker; list both components in its selector."
	fi
done

# A render with the ServiceMonitor has to exist, or the check above would pass
# by finding nothing to look at.
if [ "$monitors" -eq 0 ]; then
	fail "no golden render enables telemetry.serviceMonitor; enable it in one in the helm-template target."
fi

[ "$failed" -eq 0 ] || exit 1
echo "chart: the api and the worker each have a metrics target the ServiceMonitor scrapes"
