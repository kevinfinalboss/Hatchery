package modsource

import (
	"archive/zip"
	"bytes"
	"sort"
	"strings"
	"testing"
)

// jar builds a jar (zip) in memory from name -> content.
func jar(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write(content)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sorted(s []string) string {
	s = append([]string(nil), s...)
	sort.Strings(s)
	return strings.Join(s, ",")
}

func TestReadJarMetaFabricWithNestedJars(t *testing.T) {
	nested := jar(t, map[string][]byte{"fabric.mod.json": []byte(`{"id":"fabric-permissions-api-v0","depends":{"fabric-api-base":"*"}}`)})
	spark := jar(t, map[string][]byte{
		"fabric.mod.json": []byte(`{"schemaVersion":1,"id":"spark","provides":["spark-api"],
			"depends":{"fabricloader":">=0.15","minecraft":"*","java":">=21","fabric-api-base":"*","fabric-command-api-v2":["*"]},
			"jars":[{"file":"META-INF/jars/perms.jar"}]}`),
		"META-INF/jars/perms.jar": nested,
	})
	m := ReadJarMeta(spark)
	if got := sorted(m.Provides); got != "fabric-permissions-api-v0,spark,spark-api" {
		t.Errorf("provides = %s", got)
	}
	if got := sorted(m.Depends); got != "fabric-api-base,fabric-command-api-v2" {
		t.Errorf("depends (platform ids dropped) = %s", got)
	}
}

func TestReadJarMetaPlugin(t *testing.T) {
	p := jar(t, map[string][]byte{"plugin.yml": []byte("name: MyShop\nprovides: [Shop]\ndepend: [Vault, LuckPerms]\nsoftdepend: [PlaceholderAPI]\n")})
	m := ReadJarMeta(p)
	if sorted(m.Provides) != "MyShop,Shop" || sorted(m.Depends) != "LuckPerms,Vault" {
		t.Errorf("meta = %+v", m)
	}
	paper := jar(t, map[string][]byte{"paper-plugin.yml": []byte("name: PaperOnly\n")})
	if got := ReadJarMeta(paper); sorted(got.Provides) != "PaperOnly" {
		t.Errorf("paper-plugin.yml: %+v", got)
	}
}

func TestReadJarMetaGarbageIsEmpty(t *testing.T) {
	if m := ReadJarMeta([]byte("not a zip")); len(m.Provides)+len(m.Depends) != 0 {
		t.Errorf("garbage: %+v", m)
	}
	if m := ReadJarMeta(jar(t, map[string][]byte{"fabric.mod.json": []byte("{broken")})); len(m.Provides)+len(m.Depends) != 0 {
		t.Errorf("broken json: %+v", m)
	}
}
