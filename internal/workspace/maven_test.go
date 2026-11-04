// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package workspace_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/release-me/internal/workspace"
)

// apiPom is the pom.xml of a service module in a Maven multi-module
// build, depending on the build's parent, core and model modules in every
// way a pom.xml can.
const apiPom = `<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <parent>
    <groupId>com.acme</groupId>
    <artifactId>acme-parent</artifactId>
    <version>3.0.0</version>
  </parent>
  <artifactId>acme-api</artifactId>
  <version>0.4.0</version>
  <dependencyManagement>
    <dependencies>
      <dependency>
        <groupId>com.acme</groupId>
        <artifactId>acme-core</artifactId>
        <version>1.2.0</version>
      </dependency>
    </dependencies>
  </dependencyManagement>
  <dependencies>
    <dependency>
      <groupId>${project.groupId}</groupId>
      <artifactId>acme-core</artifactId>
      <version> 1.2.0 </version>
      <exclusions>
        <exclusion>
          <groupId>com.acme</groupId>
          <artifactId>acme-core</artifactId>
        </exclusion>
      </exclusions>
    </dependency>
    <dependency>
      <groupId>com.acme</groupId>
      <artifactId>acme-model</artifactId>
      <version>${model.version}</version>
    </dependency>
    <dependency>
      <groupId>org.slf4j</groupId>
      <artifactId>slf4j-api</artifactId>
      <version>2.0.16</version>
    </dependency>
  </dependencies>
  <build>
    <plugins>
      <plugin>
        <artifactId>maven-shade-plugin</artifactId>
        <dependencies>
          <dependency>
            <groupId>com.acme</groupId>
            <artifactId>acme-core</artifactId>
            <version>1.2.0</version>
          </dependency>
        </dependencies>
      </plugin>
    </plugins>
  </build>
</project>
`

func TestParseReadsAPomsCoordinatesAndDependencies(t *testing.T) {
	t.Parallel()
	got, err := workspace.Parse(workspace.Maven, []byte(apiPom))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	want := workspace.Package{
		Name: "com.acme:acme-api", Version: "0.4.0",
		Dependencies: []workspace.Dependency{
			{Name: "com.acme:acme-parent", Range: "3.0.0"},
			{Name: "com.acme:acme-core", Range: "1.2.0"},
			{Name: "com.acme:acme-model", Range: "${model.version}"},
			{Name: "org.slf4j:slf4j-api", Range: "2.0.16"},
			{Name: "com.acme:acme-core", Range: "1.2.0"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Parse() = %+v, want %+v", got, want)
	}
}

func TestParseRejectsAPomWithoutArtifactID(t *testing.T) {
	t.Parallel()
	if _, err := workspace.Parse(workspace.Maven, []byte("<project><groupId>com.acme</groupId></project>")); err == nil {
		t.Fatal("Parse() error = nil, want an error")
	}
}

func TestSetDependencyMovesEveryPomDeclarationOfTheModule(t *testing.T) {
	t.Parallel()
	got, err := workspace.SetDependency(workspace.Maven, apiPom, "com.acme:acme-core", "1.3.0")
	if err != nil {
		t.Fatalf("SetDependency() error = %v", err)
	}
	want := strings.Replace(apiPom, "<version>1.2.0</version>", "<version>1.3.0</version>", 1)
	want = strings.Replace(want, "<version> 1.2.0 </version>", "<version> 1.3.0 </version>", 1)
	if got != want {
		t.Fatalf("SetDependency() =\n%s\nwant\n%s", got, want)
	}
}

func TestSetDependencyMovesAPomsParentAndLeavesAPropertyAlone(t *testing.T) {
	t.Parallel()
	got, err := workspace.SetDependency(workspace.Maven, apiPom, "com.acme:acme-parent", "3.1.0")
	if err != nil {
		t.Fatalf("SetDependency() error = %v", err)
	}
	if want := strings.Replace(apiPom, "<version>3.0.0</version>", "<version>3.1.0</version>", 1); got != want {
		t.Fatalf("SetDependency() =\n%s\nwant the parent version moved only", got)
	}
	got, err = workspace.SetDependency(workspace.Maven, apiPom, "com.acme:acme-model", "2.0.0")
	if err != nil || got != apiPom {
		t.Fatalf("SetDependency() = %v, want a property version left alone", err)
	}
}

func TestSetDependencyLeavesAPomsOwnVersionAlone(t *testing.T) {
	t.Parallel()
	got, err := workspace.SetDependency(workspace.Maven, apiPom, "com.acme:acme-api", "0.5.0")
	if err != nil || got != apiPom {
		t.Fatalf("SetDependency() error = %v, want the module's own version left to its files", err)
	}
}

func TestParseIncludesProfileDependenciesInTheWorkspaceGraph(t *testing.T) {
	t.Parallel()
	dependency := `<dependency><groupId>com.acme</groupId><artifactId>acme-core</artifactId><version>1.2.0</version></dependency>`
	for _, tt := range []struct {
		name, body string
		want       bool
	}{
		{"profile dependency", "<dependencies>" + dependency + "</dependencies>", true},
		{"profile dependency management", "<dependencyManagement><dependencies>" + dependency + "</dependencies></dependencyManagement>", true},
		{"active by default profile", "<activation><activeByDefault>true</activeByDefault></activation><dependencies>" + dependency + "</dependencies>", true},
		{"profile plugin dependency", "<build><plugins><plugin><artifactId>compiler</artifactId><dependencies>" + dependency + "</dependencies></plugin></plugins></build>", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			content := `<project><groupId>com.acme</groupId><artifactId>acme-api</artifactId><version>0.4.0</version><profiles><profile><id>production</id>` + tt.body + `</profile></profiles></project>`
			pkg, err := workspace.Parse(workspace.Maven, []byte(content))
			if err != nil {
				t.Fatal(err)
			}
			var want []workspace.Dependency
			if tt.want {
				want = []workspace.Dependency{{Name: "com.acme:acme-core", Range: "1.2.0"}}
			}
			if !reflect.DeepEqual(pkg.Dependencies, want) {
				t.Fatalf("dependencies = %+v, want %+v", pkg.Dependencies, want)
			}
		})
	}
}
