import Hero from "@/components/hero";
import FeatureGrid from "@/components/feature-grid";
import ArchitectureDiagram from "@/components/architecture-diagram";
import CodeExample from "@/components/code-example";
import StatsBar from "@/components/stats-bar";
import CTASection from "@/components/cta-section";

export default function HomePage() {
  return (
    <>
      <Hero />
      <StatsBar />
      <FeatureGrid />
      <ArchitectureDiagram />
      <CodeExample />
      <CTASection />
    </>
  );
}
