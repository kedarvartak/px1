type OverviewProps = {
  params: Promise<{ organization: string }>;
};

export default async function Overview({ params }: OverviewProps) {
  const { organization } = await params;
  return (
    <main>
      <p>Organization</p>
      <h1>{organization}</h1>
      <p>The hosted dashboard foundation is ready for authenticated data.</p>
    </main>
  );
}
