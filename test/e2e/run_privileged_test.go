package integration

import (
	"os"
	"strconv"
	"strings"

	. "github.com/containers/podman/v3/test/utils"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gexec"
	"github.com/syndtr/gocapability/capability"
)

// helper function for confirming that container capabilities are equal
// to those of the host, but only to the extent of caps we (podman)
// know about at compile time. That is: the kernel may have more caps
// available than we are aware of, leading to host=FFF... and ctr=3FF...
// because the latter is all we request. Accept that.
func containerCapMatchesHost(ctrCap string, hostCap string) {
	if isRootless() {
		return
	}
	ctrCapN, err := strconv.ParseUint(ctrCap, 16, 64)
	Expect(err).NotTo(HaveOccurred(), "Error parsing %q as hex", ctrCap)

	hostCapN, err := strconv.ParseUint(hostCap, 16, 64)
	Expect(err).NotTo(HaveOccurred(), "Error parsing %q as hex", hostCap)

	// host caps can never be zero (except rootless).
	// and host caps must always be a superset (inclusive) of container
	Expect(hostCapN).To(BeNumerically(">", 0), "host cap %q should be nonzero", hostCap)
	Expect(hostCapN).To(BeNumerically(">=", ctrCapN), "host cap %q should never be less than container cap %q", hostCap, ctrCap)
	hostCapMasked := hostCapN & (1<<len(capability.List()) - 1)
	Expect(ctrCapN).To(Equal(hostCapMasked), "container cap %q is not a subset of host cap %q", ctrCap, hostCap)
}

var _ = Describe("Podman privileged container tests", func() {
	var (
		tempdir    string
		err        error
		podmanTest *PodmanTestIntegration
	)

	BeforeEach(func() {
		tempdir, err = CreateTempDirInTempDir()
		if err != nil {
			os.Exit(1)
		}
		podmanTest = PodmanTestCreate(tempdir)
		podmanTest.Setup()
		podmanTest.SeedImages()
	})

	AfterEach(func() {
		podmanTest.Cleanup()
		f := CurrentGinkgoTestDescription()
		processTestResult(f)

	})

	It("podman privileged make sure sys is mounted rw", func() {
		session := podmanTest.Podman([]string{"run", "--privileged", BB, "mount"})
		session.WaitWithDefaultTimeout()
		Expect(session).Should(Exit(0))
		ok, lines := session.GrepString("sysfs")
		Expect(ok).To(BeTrue())
		Expect(lines[0]).To(ContainSubstring("sysfs (rw,"))
	})

	It("podman privileged CapEff", func() {
		hostCap := SystemExec("awk", []string{"/^CapEff/ { print $2 }", "/proc/self/status"})
		Expect(hostCap).Should(Exit(0))

		session := podmanTest.Podman([]string{"run", "--privileged", BB, "awk", "/^CapEff/ { print $2 }", "/proc/self/status"})
		session.WaitWithDefaultTimeout()
		Expect(session).Should(Exit(0))

		containerCapMatchesHost(session.OutputToString(), hostCap.OutputToString())
	})

	It("podman cap-add CapEff", func() {
		// Get caps of current process
		hostCap := SystemExec("awk", []string{"/^CapEff/ { print $2 }", "/proc/self/status"})
		Expect(hostCap).Should(Exit(0))

		session := podmanTest.Podman([]string{"run", "--cap-add", "all", BB, "awk", "/^CapEff/ { print $2 }", "/proc/self/status"})
		session.WaitWithDefaultTimeout()
		Expect(session).Should(Exit(0))

		containerCapMatchesHost(session.OutputToString(), hostCap.OutputToString())
	})

	It("podman cap-add CapEff with --user", func() {
		// Get caps of current process
		hostCap := SystemExec("awk", []string{"/^CapEff/ { print $2 }", "/proc/self/status"})
		Expect(hostCap).Should(Exit(0))

		session := podmanTest.Podman([]string{"run", "--user=bin", "--cap-add", "all", BB, "awk", "/^CapEff/ { print $2 }", "/proc/self/status"})
		session.WaitWithDefaultTimeout()
		Expect(session).Should(Exit(0))

		containerCapMatchesHost(session.OutputToString(), hostCap.OutputToString())
	})

	It("podman cap-drop CapEff", func() {
		session := podmanTest.Podman([]string{"run", "--cap-drop", "all", BB, "grep", "CapEff", "/proc/self/status"})
		session.WaitWithDefaultTimeout()
		Expect(session).Should(Exit(0))
		capEff := strings.Split(session.OutputToString(), " ")
		Expect("0000000000000000").To(Equal(capEff[1]))
	})

	It("podman privileged should disable seccomp by default", func() {
		hostSeccomp := SystemExec("grep", []string{"-Ei", "^Seccomp:\\s+0$", "/proc/self/status"})
		Expect(hostSeccomp).Should(Exit(0))

		session := podmanTest.Podman([]string{"run", "--privileged", ALPINE, "grep", "-Ei", "^Seccomp:\\s+0$", "/proc/self/status"})
		session.WaitWithDefaultTimeout()
		Expect(session).Should(Exit(0))
	})

	It("podman non-privileged should have very few devices", func() {
		session := podmanTest.Podman([]string{"run", "-t", BB, "ls", "-l", "/dev"})
		session.WaitWithDefaultTimeout()
		Expect(session).Should(Exit(0))
		Expect(len(session.OutputToStringArray())).To(Equal(17))
	})

	It("podman privileged should inherit host devices", func() {
		session := podmanTest.Podman([]string{"run", "--privileged", ALPINE, "ls", "-l", "/dev"})
		session.WaitWithDefaultTimeout()
		Expect(session).Should(Exit(0))
		Expect(len(session.OutputToStringArray())).To(BeNumerically(">", 20))
	})

	It("run no-new-privileges test", func() {
		// Check if our kernel is new enough
		k, err := IsKernelNewerThan("4.14")
		Expect(err).To(BeNil())
		if !k {
			Skip("Kernel is not new enough to test this feature")
		}

		cap := SystemExec("grep", []string{"NoNewPrivs", "/proc/self/status"})
		if cap.ExitCode() != 0 {
			Skip("Can't determine NoNewPrivs")
		}

		session := podmanTest.Podman([]string{"run", BB, "grep", "NoNewPrivs", "/proc/self/status"})
		session.WaitWithDefaultTimeout()
		Expect(session).Should(Exit(0))

		privs := strings.Split(session.OutputToString(), ":")
		session = podmanTest.Podman([]string{"run", "--security-opt", "no-new-privileges", BB, "grep", "NoNewPrivs", "/proc/self/status"})
		session.WaitWithDefaultTimeout()
		Expect(session).Should(Exit(0))

		noprivs := strings.Split(session.OutputToString(), ":")
		Expect(privs[1]).To(Not(Equal(noprivs[1])))
	})

})
