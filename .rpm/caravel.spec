Name:           caravel
Version:        %{appversion}
Release:        1%{?dist}
Summary:        Self-hosted trip planner

License:        AGPL-3.0-or-later
URL:            https://github.com/lkiesow/caravel
Source0:        caravel
Source1:        caravel.conf
Source2:        caravel.service

# No BuildArch. The architecture comes from `rpmbuild --target`, which is what
# lets one x86_64 container produce both packages -- nothing is compiled here.
# Setting BuildArch to %{_target_cpu} as well makes rpmbuild refuse the foreign
# target outright, with "No compatible architectures found for build".

BuildRequires:  systemd-rpm-macros
Requires(pre):  shadow-utils
%{?systemd_requires}

%description
Caravel is a self-hosted trip planner: create a trip, fill it with locations to
visit, places to stay and transportation, lay them out on a map, build a
day-by-day itinerary, split the costs, and attach documents and photos along the
way.

The frontend is embedded in the binary and the only runtime dependency is a
database -- SQLite by default, with nothing to install. Configuration is read
from /etc/caravel/caravel.conf; documentation is at
https://lkiesow.github.io/caravel/

%prep
# Nothing to unpack: the sources are one binary and two text files.

%build
# Nothing to build. The binary is cross-compiled by CI and only packaged here.

%install
install -D -m 0755 %{SOURCE0} %{buildroot}%{_bindir}/%{name}
install -D -m 0640 %{SOURCE1} %{buildroot}%{_sysconfdir}/%{name}/%{name}.conf
install -D -m 0644 %{SOURCE2} %{buildroot}%{_unitdir}/%{name}.service
# Owned here as well as created by StateDirectory= in the unit, so that rpm
# knows the directory exists. Note it deliberately survives an erase once it has
# data in it: rpm will not remove a non-empty directory, which is the behaviour
# you want for somebody's trips.
install -d -m 0750 %{buildroot}%{_sharedstatedir}/%{name}

%pre
getent group %{name} >/dev/null || groupadd -r %{name}
getent passwd %{name} >/dev/null || \
    useradd -r -g %{name} -d %{_sharedstatedir}/%{name} -s /sbin/nologin \
            -c "Caravel trip planner" %{name}
exit 0

%post
%systemd_post %{name}.service

%preun
%systemd_preun %{name}.service

%postun
%systemd_postun_with_restart %{name}.service

%files
%license LICENSE
%doc README.md
%{_bindir}/%{name}
%{_unitdir}/%{name}.service
%dir %attr(0750, root, %{name}) %{_sysconfdir}/%{name}
%config(noreplace) %attr(0640, root, %{name}) %{_sysconfdir}/%{name}/%{name}.conf
%dir %attr(0750, %{name}, %{name}) %{_sharedstatedir}/%{name}

%changelog
# Deliberately empty: the release notes on GitHub are the changelog, and a
# duplicate maintained by hand here would be the one that goes stale.
